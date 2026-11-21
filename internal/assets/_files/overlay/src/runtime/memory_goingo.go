//go:build goingo

package runtime

// Memory layout of a go-in-go module, replacing baremetal_memory.go (which
// go-in-go excludes for this target). The loader reserves address space for
// the heap up to its maximum size (_heap_max), without committing memory:
// the heap starts at _heap_start.._heap_end and grows by about a third at a
// time when a collection doesn't free enough, like TinyGo on Unix.

import "unsafe"

//go:extern _heap_start
var heapStartSymbol [0]byte

//go:extern _heap_end
var heapEndSymbol [0]byte

//go:extern _heap_max
var heapMaxSymbol [0]byte

//go:extern _stack_top
var stackTopSymbol [0]byte

var (
	heapStart = uintptr(unsafe.Pointer(&heapStartSymbol))
	heapEnd   = uintptr(unsafe.Pointer(&heapEndSymbol))
	heapMax   = uintptr(unsafe.Pointer(&heapMaxSymbol))
	stackTop  = uintptr(unsafe.Pointer(&stackTopSymbol))

	// Unused with the conservative GC's compiler-generated global roots,
	// but referenced by some configurations.
	globalsStart uintptr
	globalsEnd   uintptr
)

func growHeap() bool {
	if heapEnd >= heapMax {
		return false
	}
	newEnd := heapStart + ((heapEnd-heapStart)*4/3)&^4095
	if newEnd <= heapEnd {
		newEnd = heapEnd + 4096
	}
	if newEnd > heapMax {
		newEnd = heapMax
	}
	setHeapEnd(newEnd)
	return true
}
