// Test module: every boundary type in both directions, and the edge cases.
package main

import (
	"errors"
	"strings"
	"time"
	"unsafe"
)

//gig:import
func HostEcho(b bool, i int, i8 int8, i16 int16, i32 int32, i64 int64, u uint, u8 uint8, u16 uint16, u32 uint32, u64 uint64, up uintptr, f32 float32, f64 float64, s string, bs []byte) (bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64, string, []byte, error)

//gig:import
func HostReenter() error

//gig:import
func HostPanic()

// Echo sends its arguments through HostEcho and returns what came back.
//
//gig:export
func Echo(b bool, i int, i8 int8, i16 int16, i32 int32, i64 int64, u uint, u8 uint8, u16 uint16, u32 uint32, u64 uint64, up uintptr, f32 float32, f64 float64, s string, bs []byte) (bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64, string, []byte, error) {
	return HostEcho(b, i, i8, i16, i32, i64, u, u8, u16, u32, u64, up, f32, f64, s, bs)
}

//gig:export
func Fail(msg string) error {
	if msg == "" {
		return nil
	}
	return errors.New(msg)
}

//gig:export
func Reenter() string {
	if err := HostReenter(); err != nil {
		return err.Error()
	}
	return "no error"
}

//gig:export
func PanicInHost() { HostPanic() }

// Sleepy sleeps in n goroutines at once and returns how many finished.
//
//gig:export
func Sleepy(n int, ms int) int {
	ch := make(chan int)
	for i := 0; i < n; i++ {
		go func() {
			time.Sleep(time.Duration(ms) * time.Millisecond)
			ch <- 1
		}()
	}
	total := 0
	for i := 0; i < n; i++ {
		total += <-ch
	}
	return total
}

//gig:export
func Big(n int) string { return strings.Repeat("x", n) }

//gig:export
func Len(s string, b []byte) int { return len(s) + len(b) }

var kept []string

var sink int

// Keep stores s in a global: it must survive the module's collections even
// though the host's copy is gone.
//
//gig:export
func Keep(s string) int {
	kept = append(kept, s)
	for i := 0; i < 20; i++ {
		garbage := make([][]byte, 0, 100)
		for j := 0; j < 100; j++ {
			b := make([]byte, 4096)
			b[j] = byte(i)
			garbage = append(garbage, b)
		}
		for j, b := range garbage {
			sink += int(b[j])
		}
	}
	return len(kept)
}

//gig:export
func Kept(i int) string { return kept[i] }

//gig:export
func Forever() {
	select {}
}

//gig:import
func HostTick()

// StartTicker starts a goroutine calling HostTick every ms milliseconds.
//
//gig:export
func StartTicker(ms int) {
	go func() {
		for {
			time.Sleep(time.Duration(ms) * time.Millisecond)
			HostTick()
		}
	}()
}

// Peek reads memory at addr: a fault for a bad address.
//
//gig:export
func Peek(addr uintptr) int { return *(*int)(unsafe.Pointer(addr)) }

var held [][]byte

// Hold keeps mb more MiB alive and returns the total held.
//
//gig:export
func Hold(mb int) int {
	for i := 0; i < mb; i++ {
		b := make([]byte, 1<<20)
		b[i] = 1
		held = append(held, b)
	}
	return len(held)
}

// Recurse recurses n levels deep (a stack overflow for large n).
//
//gig:export
func Recurse(n int) int { return recurse(n, nil) }

//go:noinline
func recurse(n int, prev *[64]byte) int {
	var pad [64]byte
	pad[n%64] = byte(n)
	if prev != nil {
		pad[1] += prev[n%64]
	}
	if n == 0 {
		return int(pad[1])
	}
	return recurse(n-1, &pad) + int(pad[n%64])
}
