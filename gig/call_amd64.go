package gig

import _ "unsafe" // go:linkname

func enter(ctx, fn, a0, a1 uintptr)
func resume(ctx uintptr)
func yieldTramp()
func yieldAddr() uintptr

// While the module runs, the goroutine is in the same state as during a
// blocking system call: its P can be handed to other goroutines and the
// garbage collector doesn't wait for it. Both functions are kept accessible
// by the runtime for exactly this kind of use (see go.dev/issue/67401).

//go:linkname entersyscall runtime.entersyscall
func entersyscall()

//go:linkname exitsyscall runtime.exitsyscall
func exitsyscall()

// enterModule and resumeModule must not grow the stack between entersyscall
// and exitsyscall.

//go:nosplit
func enterModule(ctx, fn, a0, a1 uintptr) {
	entersyscall()
	enter(ctx, fn, a0, a1)
	exitsyscall()
}

//go:nosplit
func resumeModule(ctx uintptr) {
	entersyscall()
	resume(ctx)
	exitsyscall()
}
