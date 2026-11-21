package gig

import (
	"debug/elf"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// ErrFault matches (with errors.Is) a *FaultError.
var ErrFault = errors.New("gig: module crashed")

// FaultError reports a hardware fault (segmentation fault, bus error, illegal
// instruction...) in module code, e.g. a bad unsafe pointer or a stack
// overflow. The call is abandoned and the instance can't be used any more,
// but the host process keeps running.
type FaultError struct {
	Signal syscall.Signal
	Offset uintptr // faulting instruction, relative to the module's load address
	Symbol string  // function containing it, if known
	Addr   uintptr // faulting memory address (SIGSEGV, SIGBUS)
}

func (e *FaultError) Error() string {
	where := fmt.Sprintf("module+%#x", e.Offset)
	if e.Symbol != "" {
		where += " (" + e.Symbol + ")"
	}
	return fmt.Sprintf("gig: module crashed: %v at %s, address %#x", e.Signal, where, e.Addr)
}

func (e *FaultError) Is(target error) bool { return target == ErrFault }

func faultHandler()
func faultReturn()
func faultHandlerAddr() uintptr

// faultTable holds the code range of every loaded module and its context
// page; the signal handler reads it, so it's only updated with atomic stores
// (ctx last when registering, first when unregistering).
type faultEntry struct{ lo, hi, ctx uintptr }

var (
	faultTable   [1024]faultEntry
	oldHandlers  [65]uintptr
	faultMu      sync.Mutex
	faultInstall sync.Once
	faultErr     error
)

type sigaction struct {
	handler  uintptr
	flags    uint64
	restorer uintptr
	mask     uint64
}

func rtSigaction(sig int, act, old *sigaction) error {
	_, _, e := syscall.RawSyscall6(syscall.SYS_RT_SIGACTION, uintptr(sig),
		uintptr(unsafe.Pointer(act)), uintptr(unsafe.Pointer(old)), 8, 0, 0)
	if e != 0 {
		return e
	}
	return nil
}

// installFaultHandler puts faultHandler in front of the Go runtime's handler
// for the synchronous fault signals. The Go runtime installs its handlers at
// startup and leaves these alone afterwards.
func installFaultHandler() error {
	faultInstall.Do(func() {
		for _, sig := range []syscall.Signal{syscall.SIGSEGV, syscall.SIGBUS, syscall.SIGILL, syscall.SIGFPE, syscall.SIGTRAP} {
			var old sigaction
			if err := rtSigaction(int(sig), nil, &old); err != nil {
				faultErr = err
				return
			}
			if old.handler <= 1 { // SIG_DFL/SIG_IGN: not the Go runtime's; leave it
				continue
			}
			oldHandlers[sig] = old.handler
			act := old
			act.handler = faultHandlerAddr()
			if err := rtSigaction(int(sig), &act, nil); err != nil {
				faultErr = err
				return
			}
		}
	})
	return faultErr
}

func registerFaultRange(lo, hi, ctx uintptr) (int, error) {
	faultMu.Lock()
	defer faultMu.Unlock()
	for i := range faultTable {
		e := &faultTable[i]
		if atomic.LoadUintptr(&e.ctx) == 0 {
			atomic.StoreUintptr(&e.lo, lo)
			atomic.StoreUintptr(&e.hi, hi)
			atomic.StoreUintptr(&e.ctx, ctx)
			return i, nil
		}
	}
	return -1, errors.New("gig: too many loaded modules")
}

func unregisterFaultRange(i int) {
	if i >= 0 {
		atomic.StoreUintptr(&faultTable[i].ctx, 0)
	}
}

// symbolizer maps module offsets to function names (from .symtab when the
// module has one, else .dynsym).
type symbolizer []elf.Symbol

func newSymbolizer(f *elf.File) symbolizer {
	syms, err := f.Symbols()
	if err != nil || len(syms) == 0 {
		syms, _ = f.DynamicSymbols()
	}
	var funcs symbolizer
	for _, s := range syms {
		if elf.ST_TYPE(s.Info) == elf.STT_FUNC && s.Value != 0 {
			funcs = append(funcs, s)
		}
	}
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].Value < funcs[j].Value })
	return funcs
}

func (s symbolizer) lookup(off uint64) string {
	i := sort.Search(len(s), func(i int) bool { return s[i].Value > off }) - 1
	if i < 0 {
		return ""
	}
	f := s[i]
	if f.Size != 0 && off >= f.Value+f.Size {
		return ""
	}
	return fmt.Sprintf("%s+%#x", f.Name, off-f.Value)
}
