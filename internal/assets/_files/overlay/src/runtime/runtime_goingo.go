//go:build goingo

// Runtime platform layer for TinyGo code running as a go-in-go module: an ELF
// shared object loaded into a regular (gc-compiled) Go process by the gig
// loader, which owns the process. go-in-go adds this file to its mirror of
// TinyGo's src/runtime.
//
// The module never makes system calls and never touches thread-local storage
// or signals: everything it needs from the outside world (output, time,
// sleeping, aborting, user-declared host functions) is a "host call". A host
// call doesn't call into Go code: gig_yield (provided by the loader, see
// gig/call_amd64.s) saves the module's registers, switches back to the Go
// stack and returns from the loader's trampoline. The loader serves the
// request in ordinary Go, then switches back here. From the module's point of
// view, gig_yield is just a C function that returns the host's answer.
//
// Memory: the conservative collector over a fixed arena the loader maps
// (_heap_start/_heap_end). Globals are the module's writable segment
// (_globals_start/_globals_end) and the system stack ends at _stack_top; the
// loader resolves all of these symbols.
//
// Scheduling: TinyGo's cooperative tasks scheduler. Every exported call runs
// as a goroutine, and the scheduler runs (on the system stack) until that
// goroutine finishes. Other module goroutines only make progress while the
// host is inside the module.

package runtime

import (
	"internal/task"
	"unsafe"
)

// Host call numbers. Keep in sync with gig/instance.go.
const (
	gigCallWrite    = 1 // (fd, ptr, len)
	gigCallNanotime = 2 // () -> monotonic ns
	gigCallWalltime = 3 // () -> unix ns
	gigCallSleep    = 4 // (ns)
	gigCallAbort    = 5 // () never returns
	gigCallImport0  = 0x1000
)

// gig_ctx is the call context shared with the loader (a page of memory
// outside both heaps). The module only passes its address back to gig_yield.
//
//go:extern gig_ctx
var gigCtxSymbol [0]byte

// gig_yield switches to the host, which serves call id with args and fills
// rets, then switches back.
//
//export gig_yield
func gigYieldRaw(ctx unsafe.Pointer, id uintptr, args *uint64, rets *uint64)

func gigYield(id uintptr, args *uint64, rets *uint64) {
	gigYieldRaw(unsafe.Pointer(&gigCtxSymbol), id, args, rets)
}

// GigHostCall is used by generated import wrappers (through go:linkname).
func gigHostCall(index uintptr, args *uint64, rets *uint64) {
	gigYield(gigCallImport0+index, args, rets)
}

func gigHostCall0(id uintptr) uint64 {
	var rets [2]uint64
	gigYield(id, nil, &rets[0])
	return rets[0]
}

func gigHostCall1(id uintptr, a uint64) uint64 {
	args := [1]uint64{a}
	var rets [2]uint64
	gigYield(id, &args[0], &rets[0])
	return rets[0]
}

const zeroSizeAllocPtr uintptr = 16

// putchar collects runtime output (println, panic messages) and hands it to
// the host one line at a time.
var lineBuf [256]byte
var lineLen int

func putchar(c byte) {
	lineBuf[lineLen] = c
	lineLen++
	if c == '\n' || lineLen == len(lineBuf) {
		flushLine()
	}
}

func flushLine() {
	if lineLen == 0 {
		return
	}
	args := [3]uint64{2, uint64(uintptr(unsafe.Pointer(&lineBuf[0]))), uint64(lineLen)}
	var rets [2]uint64
	gigYield(gigCallWrite, &args[0], &rets[0])
	lineLen = 0
}

func getchar() byte { return 0 }

func buffered() int { return 0 }

// abort is called on a fatal runtime error (after the panic message has been
// printed). The host marks the instance as dead and never switches back.
func abort() {
	flushLine()
	gigHostCall0(gigCallAbort)
	for {
	}
}

func exit(code int) {
	abort()
}

func mainReturnExit() {}

//go:linkname procPin sync/atomic.runtime_procPin
func procPin() {}

//go:linkname procUnpin sync/atomic.runtime_procUnpin
func procUnpin() {}

// Time: ticks are nanoseconds of the host's monotonic clock.
func ticks() timeUnit { return timeUnit(gigHostCall0(gigCallNanotime)) }

func nanosecondsToTicks(ns int64) timeUnit { return timeUnit(ns) }

func ticksToNanoseconds(t timeUnit) int64 { return int64(t) }

func sleepTicks(d timeUnit) {
	gigHostCall1(gigCallSleep, uint64(d))
}

var gigInitialized bool

// gig_init sets up the heap and runs package initializers and main.main. It
// returns 0, or 1 if called twice.
//
//export gig_init
func gigInit() int {
	if gigInitialized {
		return 1
	}
	gigInitialized = true
	initHeap()
	timeOffset.Store(int64(gigHostCall0(gigCallWalltime)) - nanotime())
	initRand()
	done := false
	go func() {
		initAll()
		callMain()
		done = true
	}()
	gigRun(&done)
	return 0
}

// gigRun runs the scheduler until *done becomes true. It's called on the
// system stack, by gig_init and by the generated wrappers of exported
// functions (through go:linkname). It's TinyGo's scheduler loop with an
// extra exit condition: an exported call returns as soon as its own goroutine
// is done, even if other goroutines are runnable or sleeping.
func gigRun(done *bool) {
	for !*done {
		if gigStep() {
			continue
		}
		timeLeft := gigTimeLeft()
		if timeLeft < 0 {
			runtimePanic(plainError("deadlock: exported call blocked forever"))
		}
		if timeLeft > 0 {
			sleepTicks(timeLeft)
		}
	}
}

// gigStep wakes up due sleepers and timers, then runs one runnable
// goroutine until it blocks. It returns false if nothing was runnable.
func gigStep() bool {
	if sleepQueue != nil || timerQueue != nil {
		now := ticks()
		if sleepQueue != nil && now-sleepQueueBaseTime >= timeUnit(sleepQueue.Data) {
			t := sleepQueue
			sleepQueueBaseTime += timeUnit(t.Data)
			sleepQueue = t.Next
			t.Next = nil
			runqueue.Push(t)
		}
		if timerQueue != nil && now >= timerQueue.whenTicks() {
			delay := ticksToNanoseconds(now - timerQueue.whenTicks())
			tn := timerQueue
			timerQueue = tn.next
			tn.next = nil
			tn.callback(tn, delay)
		}
	}
	t := runqueue.Pop()
	if t == nil {
		return false
	}
	t.Resume()
	return true
}

// gigTimeLeft returns the time until the next sleeper or timer is due (0 if
// one is due now), or -1 if there are none.
func gigTimeLeft() timeUnit {
	if sleepQueue == nil && timerQueue == nil {
		return -1
	}
	now := ticks()
	timeLeft := timeUnit(-1)
	if sleepQueue != nil {
		timeLeft = timeUnit(sleepQueue.Data) - (now - sleepQueueBaseTime)
	}
	if timerQueue != nil {
		if tl := timerQueue.whenTicks() - now; timeLeft < 0 || tl < timeLeft {
			timeLeft = tl
		}
	}
	if timeLeft < 0 {
		timeLeft = 0
	}
	return timeLeft
}

// gig_poll runs the module's goroutines (those started by earlier calls and
// still running) until none is runnable, without waiting for sleeping ones.
// It returns the nanoseconds until the next sleeper or timer is due, or -1
// if there are none: the host calls it again by then.
//
//export gig_poll
func gigPoll() int64 {
	for gigStep() {
	}
	return ticksToNanoseconds(gigTimeLeft())
}

// Exported calls run on worker goroutines that are reused: starting a new
// goroutine per call would allocate (and later collect) a whole goroutine
// stack every time, which costs ~100x more than the call itself.
type gigWorker struct {
	t    *task.Task
	fn   func()
	done bool
	next *gigWorker
}

var gigIdleWorkers *gigWorker

// gigExec runs fn on a worker goroutine and runs the scheduler (on the system
// stack) until it's done. Generated export wrappers call it through
// go:linkname.
func gigExec(fn func()) {
	w := gigIdleWorkers
	if w != nil {
		gigIdleWorkers = w.next
		w.next = nil
		w.fn = fn
		w.done = false
		scheduleTask(w.t)
	} else {
		w = &gigWorker{fn: fn}
		go gigWorkerLoop(w)
	}
	gigRun(&w.done)
}

func gigWorkerLoop(w *gigWorker) {
	w.t = task.Current()
	for {
		w.fn()
		w.fn = nil
		w.done = true
		w.next = gigIdleWorkers
		gigIdleWorkers = w
		task.Pause() // until gigExec hands this worker a new call
	}
}

// gig_heap_stats reports heap usage to the host: bytes in use, arena size,
// bytes allocated so far and number of collections.
//
//export gig_heap_stats
func gigHeapStats(out *[4]uint64) {
	var ms MemStats
	ReadMemStats(&ms)
	out[0] = ms.HeapAlloc
	out[1] = ms.HeapSys
	out[2] = ms.TotalAlloc
	out[3] = uint64(ms.NumGC)
}
