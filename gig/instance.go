// Package gig loads go-in-go modules (TinyGo code compiled into a
// freestanding x86-64 ELF shared object) into a Go program and calls them.
//
// No cgo, no dlopen and no libc are involved: gig maps and relocates the
// module itself and switches stacks with a few lines of assembly. The module
// brings its own runtime (garbage collector, scheduler) and never shares
// memory, types or runtime state with the host, so a module built once works
// with any Go version the host is compiled with.
package gig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// Host call numbers. Keep in sync with runtime_goingo.go.
const (
	callWrite    = 1
	callNanotime = 2
	callWalltime = 3
	callSleep    = 4
	callAbort    = 5
	callImport0  = 0x1000
)

// Layout of the context page (see call_amd64.s for the first words).
const (
	ctxStatus  = 2
	ctxID      = 3
	ctxArgs    = 4
	ctxRets    = 5
	ctxStack   = 6
	ctxRet     = 7
	ctxArgsOff = 512  // export call argument slots
	ctxRetsOff = 1024 // export call result slots
	ctxStatOff = 1536 // heap stats
)

var (
	// ErrAborted is returned when the module panicked or hit a fatal runtime
	// error. The instance can't be used any more.
	ErrAborted = errors.New("gig: module aborted (panic or fatal error)")
	// ErrClosed is returned by calls on a closed instance.
	ErrClosed = errors.New("gig: instance closed")
	// ErrReentrant is returned when a host function calls back into the
	// instance that's calling it.
	ErrReentrant = errors.New("gig: reentrant call into a module from one of its host functions")
)

// HostFunc implements a function a module imports.
type HostFunc func(c *HostCall)

// Import is a host function with the signature it's expected to have (as
// Func.Signature); an empty Sig skips the check.
type Import struct {
	Sig string
	Fn  HostFunc
}

// Options configure a module instance.
type Options struct {
	HeapSize  int // initial module heap; default 4 MiB
	HeapMax   int // the heap grows up to this; default 1 GiB (address space only)
	StackSize int // module system stack; default 1 MiB

	// Background keeps the module's goroutines running between calls: a
	// host goroutine runs them whenever they're runnable or a sleeper is
	// due. Without it, module goroutines only run while a call is in
	// progress.
	Background bool

	Stdout    io.Writer
	Stderr    io.Writer
	Imports   map[string]Import
}

// Instance is a loaded and initialized module. Calls into one instance are
// serialized: the module runs on one thread at a time.
type Instance struct {
	mu       sync.Mutex
	manifest *Manifest
	img      *image
	heapMap  []byte // guard page + heap reservation
	heap     []byte // the heap reservation (up to HeapMax)
	stack    []byte
	ctxPage  []byte
	ctx      uintptr
	exports  []uintptr // by manifest export index
	byName   map[string]int
	imports  []HostFunc // by manifest import index
	scratch  scratch
	stdout   io.Writer
	stderr   io.Writer
	start    time.Time
	dead     error
	frame    Frame
	inHost   atomic.Bool  // serving a user host function
	hostG    atomic.Int64 // goroutine serving it
	initDone bool
	fault    int // slot in faultTable
	pollFn   uintptr
	kick     chan struct{} // Background: wake the pump
	stop     chan struct{} // Background: stop the pump
}

// Load reads, loads and initializes the module at path.
func Load(path string, opts *Options) (*Instance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data, opts)
}

// LoadBytes loads and initializes a module from the contents of its file. It
// runs the module's package initializers and main function.
func LoadBytes(data []byte, opts *Options) (*Instance, error) {
	if opts == nil {
		opts = &Options{}
	}
	m, err := ReadManifest(data)
	if err != nil {
		return nil, err
	}
	if err := installFaultHandler(); err != nil {
		return nil, fmt.Errorf("gig: installing the fault handler: %w", err)
	}
	in := &Instance{
		fault:    -1,
		manifest: m,
		byName:   map[string]int{},
		stdout:   opts.Stdout,
		stderr:   opts.Stderr,
		start:    time.Now(),
	}
	if in.stdout == nil {
		in.stdout = os.Stdout
	}
	if in.stderr == nil {
		in.stderr = os.Stderr
	}
	ok := false
	defer func() {
		if !ok {
			in.unmap()
		}
	}()

	for _, f := range m.Imports {
		imp, found := opts.Imports[f.Name]
		if !found || imp.Fn == nil {
			return nil, fmt.Errorf("gig: module imports %s%s, which the host doesn't provide", f.Name, f.Signature())
		}
		if imp.Sig != "" && imp.Sig != f.Signature() {
			return nil, fmt.Errorf("gig: module imports %s%s, the host provides %s%s", f.Name, f.Signature(), f.Name, imp.Sig)
		}
		in.imports = append(in.imports, imp.Fn)
	}

	heapSize := opts.HeapSize
	if heapSize <= 0 {
		heapSize = 4 << 20
	}
	heapMax := opts.HeapMax
	if heapMax <= 0 {
		heapMax = 1 << 30
	}
	if heapMax < heapSize {
		heapMax = heapSize
	}
	stackSize := opts.StackSize
	if stackSize <= 0 {
		stackSize = 1 << 20
	}
	// A guard page below the heap: a goroutine stack (a heap object)
	// overflowing downwards faults there instead of reaching whatever is
	// mapped below.
	if in.heapMap, err = mmap(pageSize+int(roundUp(uint64(heapMax), pageSize)), true); err != nil {
		return nil, err
	}
	if err := syscall.Mprotect(in.heapMap[:pageSize], syscall.PROT_NONE); err != nil {
		return nil, err
	}
	in.heap = in.heapMap[pageSize:]
	// The system stack, with a guard page at the bottom.
	if in.stack, err = mmap(int(roundUp(uint64(stackSize), pageSize))+pageSize, true); err != nil {
		return nil, err
	}
	if err := syscall.Mprotect(in.stack[:pageSize], syscall.PROT_NONE); err != nil {
		return nil, err
	}
	if in.ctxPage, err = mmap(pageSize, false); err != nil {
		return nil, err
	}
	in.ctx = uintptr(unsafe.Pointer(&in.ctxPage[0]))
	stackTop := (uintptr(unsafe.Pointer(&in.stack[0])) + uintptr(len(in.stack))) &^ 15
	in.word(ctxStack, uint64(stackTop))

	heapStart := uintptr(unsafe.Pointer(&in.heap[0]))
	in.img, err = loadImage(data, map[string]uintptr{
		"gig_yield":   yieldAddr(),
		"gig_ctx":     in.ctx,
		"_heap_start": heapStart,
		"_heap_end":   heapStart + uintptr(roundUp(uint64(heapSize), pageSize)),
		"_heap_max":   heapStart + uintptr(len(in.heap)),
		"_stack_top":  stackTop,
	})
	if err != nil {
		return nil, fmt.Errorf("gig: loading module: %w", err)
	}
	if in.fault, err = registerFaultRange(in.img.base, in.img.base+uintptr(len(in.img.mem)), in.ctx); err != nil {
		return nil, err
	}

	for i, f := range m.Exports {
		sym := "gig_export_" + f.Name
		addr, found := in.img.symbols[sym]
		if !found {
			return nil, fmt.Errorf("gig: module manifest lists %s, but the module has no %s symbol", f.Name, sym)
		}
		in.exports = append(in.exports, addr)
		in.byName[f.Name] = i
	}
	initFn, found := in.img.symbols["gig_init"]
	if !found {
		return nil, errors.New("gig: module has no gig_init")
	}
	in.frame.in = in
	in.mu.Lock()
	_, err = in.run(initFn, 0, 0)
	in.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("gig: initializing module: %w", err)
	}
	in.initDone = true
	ok = true
	if opts.Background {
		in.pollFn = in.img.symbols["gig_poll"]
		in.kick = make(chan struct{}, 1)
		in.stop = make(chan struct{})
		go in.pump()
		in.wake()
	}
	return in, nil
}

// Err returns why the instance can't be used any more (ErrClosed,
// ErrAborted, a *FaultError, ...), or nil.
func (in *Instance) Err() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.dead
}

// GigInstance returns in itself; generated Module types have the same
// method, so both can be used with Pool.
func (in *Instance) GigInstance() *Instance { return in }

// wake makes the background pump poll the module soon.
func (in *Instance) wake() {
	if in.kick != nil {
		select {
		case in.kick <- struct{}{}:
		default:
		}
	}
}

// pump runs the module's goroutines between calls (Options.Background).
func (in *Instance) pump() {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case <-in.stop:
			return
		case <-in.kick:
		case <-timer.C:
		}
		in.mu.Lock()
		if in.dead != nil {
			in.mu.Unlock()
			return
		}
		next, err := in.run(in.pollFn, 0, 0)
		in.mu.Unlock()
		if err != nil {
			return
		}
		if d := int64(next); d >= 0 {
			timer.Reset(time.Duration(d))
		}
	}
}

// Manifest returns the module's manifest.
func (in *Instance) Manifest() *Manifest { return in.manifest }

// Close unmaps the module. Calls after Close return ErrClosed.
func (in *Instance) Close() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.dead == ErrClosed {
		return nil
	}
	in.dead = ErrClosed
	if in.stop != nil {
		close(in.stop)
	}
	in.unmap()
	return nil
}

func (in *Instance) unmap() {
	unregisterFaultRange(in.fault)
	in.fault = -1
	for _, m := range [][]byte{in.heapMap, in.stack, in.ctxPage} {
		if m != nil {
			syscall.Munmap(m)
		}
	}
	if in.img != nil {
		syscall.Munmap(in.img.mem)
	}
	in.scratch.free()
	in.heapMap, in.heap, in.stack, in.ctxPage, in.img = nil, nil, nil, nil, nil
}

// HeapStats describes a module's heap.
type HeapStats struct {
	InUse      uint64 // bytes of live (or not yet collected) objects
	Arena      uint64 // size of the heap arena
	TotalAlloc uint64 // bytes allocated so far
	NumGC      uint64 // collections counted by TinyGo's runtime
}

// HeapStats reports on the module's heap (its own, separate from the host's).
func (in *Instance) HeapStats() (HeapStats, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.dead != nil {
		return HeapStats{}, in.dead
	}
	fn, ok := in.img.symbols["gig_heap_stats"]
	if !ok {
		return HeapStats{}, errors.New("gig: module has no gig_heap_stats")
	}
	out := in.ctx + ctxStatOff
	if _, err := in.run(fn, out, 0); err != nil {
		return HeapStats{}, err
	}
	s := (*[4]uint64)(unsafe.Pointer(out))
	return HeapStats{InUse: s[0], Arena: s[1], TotalAlloc: s[2], NumGC: s[3]}, nil
}

func (in *Instance) word(i int, v uint64) {
	*(*uint64)(unsafe.Pointer(in.ctx + uintptr(i)*8)) = v
}

func (in *Instance) getWord(i int) uint64 {
	return *(*uint64)(unsafe.Pointer(in.ctx + uintptr(i)*8))
}

// run calls fn(a0, a1) in the module and serves its host calls until it
// returns. in.mu must be held.
func (in *Instance) run(fn, a0, a1 uintptr) (uint64, error) {
	enterModule(in.ctx, fn, a0, a1)
	for {
		switch in.getWord(ctxStatus) {
		case 0:
			return in.getWord(ctxRet), nil
		case 2:
			// A fault in module code, caught by faultHandler.
			pc := uintptr(in.getWord(ctxArgs)) - in.img.base
			in.dead = &FaultError{
				Signal: syscall.Signal(in.getWord(ctxID)),
				Offset: pc,
				Symbol: in.img.funcs.lookup(uint64(pc)),
				Addr:   uintptr(in.getWord(ctxRets)),
			}
			return 0, in.dead
		}
		if err := in.hostCall(in.getWord(ctxID), uintptr(in.getWord(ctxArgs)), uintptr(in.getWord(ctxRets))); err != nil {
			// The module is suspended in the middle of a call and can't be
			// resumed: its stacks are simply abandoned.
			in.dead = err
			return 0, err
		}
		resumeModule(in.ctx)
	}
}

func (in *Instance) hostCall(id uint64, argsPtr, retsPtr uintptr) (err error) {
	switch id {
	case callWrite:
		args := slots(argsPtr, 3)
		buf, err := in.memory(args[1], args[2])
		if err != nil {
			return err
		}
		w := in.stdout
		if args[0] == 2 {
			w = in.stderr
		}
		w.Write(buf)
		slots(retsPtr, 1)[0] = args[2]
	case callNanotime:
		slots(retsPtr, 1)[0] = uint64(time.Since(in.start)) + 1
	case callWalltime:
		slots(retsPtr, 1)[0] = uint64(time.Now().UnixNano())
	case callSleep:
		time.Sleep(time.Duration(slots(argsPtr, 1)[0]))
	case callAbort:
		return ErrAborted
	default:
		idx := int(id - callImport0)
		if id < callImport0 || idx >= len(in.imports) {
			return fmt.Errorf("gig: module made an unknown host call %d", id)
		}
		f := &in.manifest.Imports[idx]
		c := &HostCall{in: in, args: slots(argsPtr, f.ArgSlots()), rets: slots(retsPtr, f.RetSlots())}
		defer func() {
			in.inHost.Store(false)
			if r := recover(); r != nil {
				if e, ok := r.(memError); ok {
					err = e
					return
				}
				err = fmt.Errorf("gig: host function %s panicked: %v", f.Name, r)
			}
		}()
		in.scratch.reset()
		in.hostG.Store(goid())
		in.inHost.Store(true)
		in.imports[idx](c)
	}
	return nil
}

func slots(p uintptr, n int) []uint64 {
	if n == 0 || p == 0 {
		return nil
	}
	return unsafe.Slice((*uint64)(unsafe.Pointer(p)), n)
}

type memError struct{ error }

// memory returns the module memory at [ptr, ptr+n) (no copy). It must lie in
// the module's image, heap, stack or the host's scratch area.
func (in *Instance) memory(ptr, n uint64) ([]byte, error) {
	if n == 0 {
		return nil, nil
	}
	stack := in.stack[pageSize:] // not the guard page
	for _, m := range [][]byte{in.heap, stack, in.img.mem, in.scratch.cur} {
		if len(m) == 0 {
			continue
		}
		base := uint64(uintptr(unsafe.Pointer(&m[0])))
		if ptr >= base && ptr+n <= base+uint64(len(m)) && ptr+n > ptr {
			return m[ptr-base : ptr-base+n], nil
		}
	}
	return nil, memError{fmt.Errorf("gig: module passed an invalid memory range %#x+%d", ptr, n)}
}

// HostCall gives a host function access to its arguments and results, as
// slots (see Types). Generated bindings use it; strings and byte slices are
// copied out of module memory.
type HostCall struct {
	in   *Instance
	args []uint64
	rets []uint64
}

// Arg returns argument slot i.
func (c *HostCall) Arg(i int) uint64 { return c.args[i] }

// String returns the string in argument slots i, i+1.
func (c *HostCall) String(i int) string {
	b, err := c.in.memory(c.args[i], c.args[i+1])
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Bytes returns a copy of the byte slice in argument slots i, i+1.
func (c *HostCall) Bytes(i int) []byte {
	b, err := c.in.memory(c.args[i], c.args[i+1])
	if err != nil {
		panic(err)
	}
	return append([]byte(nil), b...)
}

// SetRet sets result slot i.
func (c *HostCall) SetRet(i int, v uint64) { c.rets[i] = v }

// SetString sets result slots i, i+1 to s (the module copies it).
func (c *HostCall) SetString(i int, s string) {
	c.rets[i], c.rets[i+1] = c.in.scratch.put(s)
}

// SetBytes sets result slots i, i+1 to b (the module copies it).
func (c *HostCall) SetBytes(i int, b []byte) {
	c.rets[i], c.rets[i+1] = c.in.scratch.put(string(b))
}

// SetError sets result slots i, i+1 to err.
func (c *HostCall) SetError(i int, err error) {
	if err == nil {
		c.rets[i], c.rets[i+1] = 0, 0
		return
	}
	p, n := c.in.scratch.put(err.Error())
	c.rets[i], c.rets[i+1] = p, n+1
}

// Frame is an export call being prepared or finished. Generated bindings use
// it: Begin, Set*, Run, Ret*, End.
type Frame struct {
	in   *Instance
	fn   uintptr
	args *[MaxArgSlots]uint64
	rets *[MaxRetSlots]uint64
}

// Export returns the index of the exported function name, checking its
// signature (as Func.Signature) unless sig is empty.
func (in *Instance) Export(name, sig string) (int, error) {
	i, ok := in.byName[name]
	if !ok {
		return 0, fmt.Errorf("gig: module doesn't export %s", name)
	}
	if got := in.manifest.Exports[i].Signature(); sig != "" && got != sig {
		return 0, fmt.Errorf("gig: module exports %s%s, expected %s%s", name, got, name, sig)
	}
	return i, nil
}

// Begin starts a call to export idx: it locks the instance until End.
func (in *Instance) Begin(idx int) (*Frame, error) {
	if !in.mu.TryLock() {
		if in.inHost.Load() && in.hostG.Load() == goid() {
			return nil, ErrReentrant
		}
		in.mu.Lock()
	}
	if in.dead != nil {
		err := in.dead
		in.mu.Unlock()
		return nil, err
	}
	in.scratch.reset()
	f := &in.frame
	f.fn = in.exports[idx]
	f.args = (*[MaxArgSlots]uint64)(unsafe.Pointer(in.ctx + ctxArgsOff))
	f.rets = (*[MaxRetSlots]uint64)(unsafe.Pointer(in.ctx + ctxRetsOff))
	return f, nil
}

// End finishes the call and unlocks the instance.
func (f *Frame) End() {
	f.in.mu.Unlock()
	f.in.wake() // the call may have left goroutines runnable
}

// Set sets argument slot i.
func (f *Frame) Set(i int, v uint64) { f.args[i] = v }

// SetString sets argument slots i, i+1 to s (the module copies it).
func (f *Frame) SetString(i int, s string) { f.args[i], f.args[i+1] = f.in.scratch.put(s) }

// SetBytes sets argument slots i, i+1 to b (the module copies it).
func (f *Frame) SetBytes(i int, b []byte) {
	f.args[i], f.args[i+1] = f.in.scratch.put(string(b))
}

// Run runs the call.
func (f *Frame) Run() error {
	_, err := f.in.run(f.fn, uintptr(unsafe.Pointer(f.args)), uintptr(unsafe.Pointer(f.rets)))
	return err
}

// Ret returns result slot i.
func (f *Frame) Ret(i int) uint64 { return f.rets[i] }

// RetString returns the string in result slots i, i+1.
func (f *Frame) RetString(i int) (string, error) {
	b, err := f.in.memory(f.rets[i], f.rets[i+1])
	return string(b), err
}

// RetBytes returns a copy of the byte slice in result slots i, i+1.
func (f *Frame) RetBytes(i int) ([]byte, error) {
	b, err := f.in.memory(f.rets[i], f.rets[i+1])
	if err != nil || b == nil {
		return nil, err
	}
	return append([]byte(nil), b...), nil
}

// RetError returns the error in result slots i, i+1.
func (f *Frame) RetError(i int) (error, error) {
	if f.rets[i+1] == 0 {
		return nil, nil
	}
	b, err := f.in.memory(f.rets[i], f.rets[i+1]-1)
	if err != nil {
		return nil, err
	}
	return errors.New(string(b)), nil
}

// scratch is host memory outside the Go heap where values passed to the
// module are staged; the module copies them before it can run anything
// else, so it's reset at every call.
type scratch struct {
	cur  []byte
	used int
	old  [][]byte
}

func (s *scratch) put(v string) (ptr, n uint64) {
	if len(v) == 0 {
		return 0, 0
	}
	if s.used+len(v) > len(s.cur) {
		size := 64 << 10
		for size < len(v) {
			size *= 2
		}
		m, err := mmap(size, false)
		if err != nil {
			panic(err)
		}
		if s.cur != nil {
			s.old = append(s.old, s.cur) // still referenced by this call
		}
		s.cur, s.used = m, 0
	}
	copy(s.cur[s.used:], v)
	p := uintptr(unsafe.Pointer(&s.cur[s.used]))
	s.used += (len(v) + 7) &^ 7
	return uint64(p), uint64(len(v))
}

func (s *scratch) reset() {
	for _, m := range s.old {
		syscall.Munmap(m)
	}
	s.old = s.old[:0]
	s.used = 0
}

func (s *scratch) free() {
	s.reset()
	if s.cur != nil {
		syscall.Munmap(s.cur)
		s.cur = nil
	}
}

func mmap(size int, noreserve bool) ([]byte, error) {
	flags := syscall.MAP_PRIVATE | syscall.MAP_ANON
	if noreserve {
		flags |= syscall.MAP_NORESERVE
	}
	m, err := syscall.Mmap(-1, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, flags)
	if err != nil {
		return nil, fmt.Errorf("gig: mmap %d bytes: %w", size, err)
	}
	return m, nil
}

// goid returns the current goroutine's id (only used to detect reentrant
// calls, so its cost only matters for host functions).
func goid() int64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	// "goroutine 123 [running]:"
	const prefix = "goroutine "
	if len(b) < len(prefix) {
		return -1
	}
	b = b[len(prefix):]
	i := 0
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	id, _ := strconv.ParseInt(string(b[:i]), 10, 64)
	return id
}

// BoolSlot returns the slot value of b.
func BoolSlot(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}
