//go:build goingo

package interrupt

// A go-in-go module has no interrupts: module code only runs while the host
// is inside it, on one thread at a time.

// State is unused.
type State uintptr

// Disable does nothing.
func Disable() (state State) { return 0 }

// Restore does nothing.
func Restore(state State) {}

// In reports whether we're in an interrupt handler: never.
func In() bool { return false }
