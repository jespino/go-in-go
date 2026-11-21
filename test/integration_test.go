// Integration tests: build test modules with TinyGo and load them. Skipped
// when tinygo isn't installed.
package test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jespino/go-in-go/gig"
	"github.com/jespino/go-in-go/internal/build"
	"github.com/jespino/go-in-go/test/kitchen"
)

var (
	buildOnce sync.Once
	modPath   string
	buildErr  error
)

// kitchenModule builds testdata/kitchen, or uses the prebuilt module in
// $GIG_TEST_MODULE: build it once, then run the tests with any Go version:
//
//	go-in-go build -o /tmp/kitchen.gig test/testdata/kitchen
//	GIG_TEST_MODULE=/tmp/kitchen.gig GOTOOLCHAIN=go1.22.12 go test ./test
func kitchenModule(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GIG_TEST_MODULE"); p != "" {
		return p
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo not installed")
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gig-test-")
		if err != nil {
			buildErr = err
			return
		}
		var log bytes.Buffer
		modPath, buildErr = build.Build(build.Options{
			Dir:    "testdata/kitchen",
			Output: filepath.Join(dir, "kitchen.gig"),
			Log:    &log,
		})
		if buildErr != nil {
			buildErr = fmt.Errorf("%w\n%s", buildErr, log.String())
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return modPath
}

// TestBindingsUpToDate checks that the committed bindings match the module.
func TestBindingsUpToDate(t *testing.T) {
	data, err := os.ReadFile(kitchenModule(t))
	if err != nil {
		t.Fatal(err)
	}
	m, err := gig.ReadManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	src, err := build.Bindgen(m, "kitchen", "kitchen.gig")
	if err != nil {
		t.Fatal(err)
	}
	have, _ := os.ReadFile("kitchen/kitchen_gig.go")
	if !bytes.Equal(src, have) {
		if os.Getenv("UPDATE") != "" {
			os.WriteFile("kitchen/kitchen_gig.go", src, 0o644)
			return
		}
		t.Fatal("kitchen/kitchen_gig.go is stale; rerun with UPDATE=1")
	}
}

type host struct {
	m          *kitchen.Module
	reenterErr error
	ticks      atomic.Int64
}

func (h *host) HostTick() { h.ticks.Add(1) }

func (h *host) HostEcho(b bool, i int, i8 int8, i16 int16, i32 int32, i64 int64, u uint, u8 uint8, u16 uint16, u32 uint32, u64 uint64, up uintptr, f32 float32, f64 float64, s string, bs []byte) (bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64, string, []byte, error) {
	return b, i, i8, i16, i32, i64, u, u8, u16, u32, u64, up, f32, f64, s + "!", append(bs, '!'), errors.New("echo:" + s)
}

func (h *host) HostReenter() error {
	_, err := h.m.Len("a", nil)
	h.reenterErr = err
	return err
}

func (h *host) HostPanic() { panic("host function exploded") }

func load(t *testing.T) (*kitchen.Module, *host) {
	return loadOpts(t, &gig.Options{HeapSize: 16 << 20})
}

func loadOpts(t *testing.T, opts *gig.Options) (*kitchen.Module, *host) {
	h := &host{}
	m, err := kitchen.Load(kitchenModule(t), h, opts)
	if err != nil {
		t.Fatal(err)
	}
	h.m = m
	t.Cleanup(func() { m.Close() })
	return m, h
}

func TestEchoAllTypes(t *testing.T) {
	m, _ := load(t)
	for _, neg := range []bool{false, true} {
		sgn := 1
		if neg {
			sgn = -1
		}
		b, i, i8, i16, i32, i64, u, u8, u16, u32, u64, up, f32, f64, s, bs, err := m.Echo(
			neg, sgn*math.MaxInt, int8(sgn*127), int16(sgn*32767), int32(sgn*2147483647), int64(sgn)*math.MaxInt64,
			math.MaxUint, 255, 65535, math.MaxUint32, math.MaxUint64, ^uintptr(0),
			float32(sgn)*1.5, float64(sgn)*math.Pi, "héllo", []byte{0, 1, 2})
		if b != neg || i != sgn*math.MaxInt || i8 != int8(sgn*127) || i16 != int16(sgn*32767) ||
			i32 != int32(sgn*2147483647) || i64 != int64(sgn)*math.MaxInt64 || u != math.MaxUint ||
			u8 != 255 || u16 != 65535 || u32 != math.MaxUint32 || u64 != math.MaxUint64 || up != ^uintptr(0) ||
			f32 != float32(sgn)*1.5 || f64 != float64(sgn)*math.Pi || s != "héllo!" || !bytes.Equal(bs, []byte{0, 1, 2, '!'}) ||
			err == nil || err.Error() != "echo:héllo" {
			t.Fatalf("neg=%v: got %v %v %v %v %v %v %v %v %v %v %v %v %v %v %q %v %v", neg,
				b, i, i8, i16, i32, i64, u, u8, u16, u32, u64, up, f32, f64, s, bs, err)
		}
	}
	// NaN and empty values.
	_, _, _, _, _, _, _, _, _, _, _, _, f32, f64, s, bs, _ := m.Echo(false, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, float32(math.NaN()), math.Inf(-1), "", nil)
	if !math.IsNaN(float64(f32)) || !math.IsInf(f64, -1) || s != "!" || string(bs) != "!" {
		t.Fatalf("got %v %v %q %q", f32, f64, s, bs)
	}
}

func TestErrors(t *testing.T) {
	m, _ := load(t)
	if err := m.Fail(""); err != nil {
		t.Fatalf("Fail(\"\") = %v", err)
	}
	if err := m.Fail("bad thing"); err == nil || err.Error() != "bad thing" {
		t.Fatalf("Fail = %v", err)
	}
}

func TestReentrant(t *testing.T) {
	m, h := load(t)
	s, err := m.Reenter()
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(h.reenterErr, gig.ErrReentrant) || s != gig.ErrReentrant.Error() {
		t.Fatalf("got %q, host saw %v", s, h.reenterErr)
	}
	// The instance still works.
	if n, err := m.Len("abc", []byte("de")); n != 5 || err != nil {
		t.Fatalf("Len = %d, %v", n, err)
	}
}

func TestHostPanic(t *testing.T) {
	m, _ := load(t)
	err := m.PanicInHost()
	if err == nil || !strings.Contains(err.Error(), "host function exploded") {
		t.Fatalf("got %v", err)
	}
	if _, err := m.Len("", nil); err == nil {
		t.Fatal("instance should be dead")
	}
}

func TestModulePanic(t *testing.T) {
	var stderr bytes.Buffer
	m, err := kitchen.Load(kitchenModule(t), &host{}, &gig.Options{Stderr: &stderr, Stdout: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, err = m.Kept(42) // index out of range
	if !errors.Is(err, gig.ErrAborted) || !strings.Contains(stderr.String(), "index out of range") {
		t.Fatalf("got %v, output %q", err, stderr.String())
	}
}

func TestDeadlock(t *testing.T) {
	var out bytes.Buffer
	m, err := kitchen.Load(kitchenModule(t), &host{}, &gig.Options{Stderr: &out, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Forever(); !errors.Is(err, gig.ErrAborted) || !strings.Contains(out.String(), "deadlock") {
		t.Fatalf("got %v, output %q", err, out.String())
	}
}

func TestBigValues(t *testing.T) {
	m, _ := load(t)
	s, err := m.Big(5 << 20)
	if err != nil || len(s) != 5<<20 || strings.Trim(s, "x") != "" {
		t.Fatalf("Big: len %d, %v", len(s), err)
	}
	n, err := m.Len(strings.Repeat("y", 3<<20), make([]byte, 1<<20))
	if err != nil || n != 4<<20 {
		t.Fatalf("Len = %d, %v", n, err)
	}
}

// TestModuleGC: values the module keeps survive its collections.
func TestModuleGC(t *testing.T) {
	m, _ := load(t)
	for i := 0; i < 100; i++ {
		if _, err := m.Keep(strings.Repeat(string(rune('a'+i%26)), 100+i)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := m.Instance.HeapStats()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("module heap: %+v", st)
	if st.TotalAlloc < 10*st.Arena {
		t.Fatalf("expected the module to allocate well beyond its arena: %+v", st)
	}
	for i := 0; i < 100; i++ {
		s, err := m.Kept(i)
		if err != nil || s != strings.Repeat(string(rune('a'+i%26)), 100+i) {
			t.Fatalf("kept[%d] = %q, %v", i, s, err)
		}
	}
}

// TestHostGCWhileModuleRuns: the module runs (sleeping, in goroutines) while
// other host goroutines allocate and force collections. The goroutine inside
// the module must not hold up the host's stop-the-world phases.
func TestHostGCWhileModuleRuns(t *testing.T) {
	m, _ := load(t)
	var stop atomic.Bool
	var gcs atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			_ = make([]byte, 1<<20)
			runtime.GC()
			gcs.Add(1)
		}
	}()
	start := time.Now()
	n, err := m.Sleepy(20, 300)
	elapsed := time.Since(start)
	stop.Store(true)
	<-done
	if err != nil || n != 20 {
		t.Fatalf("Sleepy = %d, %v", n, err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Sleepy took %v", elapsed)
	}
	if gcs.Load() < 5 {
		t.Fatalf("only %d host GCs during a %v module call", gcs.Load(), elapsed)
	}
	t.Logf("%d host GCs during a %v module call", gcs.Load(), elapsed)
}

func TestConcurrentInstances(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		m, _ := load(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				if n, err := m.Len("abcd", []byte{1}); n != 5 || err != nil {
					t.Errorf("Len = %d, %v", n, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestClose(t *testing.T) {
	m, _ := load(t)
	m.Close()
	if _, err := m.Len("", nil); !errors.Is(err, gig.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestBadImports(t *testing.T) {
	_, err := gig.Load(kitchenModule(t), nil)
	if err == nil || !strings.Contains(err.Error(), "HostEcho") {
		t.Fatalf("got %v", err)
	}
	_, err = gig.Load(kitchenModule(t), &gig.Options{Imports: map[string]gig.Import{
		"HostEcho":    {Sig: "(int)()", Fn: func(*gig.HostCall) {}},
		"HostReenter": {Fn: func(*gig.HostCall) {}},
		"HostPanic":   {Fn: func(*gig.HostCall) {}},
	}})
	if err == nil || !strings.Contains(err.Error(), "the host provides HostEcho(int)()") {
		t.Fatalf("got %v", err)
	}
}

func BenchmarkCall(b *testing.B) {
	if _, err := exec.LookPath("tinygo"); err != nil {
		b.Skip("tinygo not installed")
	}
	m, err := kitchen.Load(kitchenModule(&testing.T{}), &host{}, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()
	for i := 0; i < b.N; i++ {
		m.Len("abc", nil)
	}
}

func TestFault(t *testing.T) {
	m, _ := load(t)
	_, err := m.Peek(0x18)
	var fe *gig.FaultError
	if !errors.As(err, &fe) || !errors.Is(err, gig.ErrFault) {
		t.Fatalf("got %v", err)
	}
	if fe.Signal != syscall.SIGSEGV || fe.Addr != 0x18 {
		t.Fatalf("got %+v", fe)
	}
	t.Log(err)
	if _, err := m.Len("", nil); !errors.Is(err, gig.ErrFault) {
		t.Fatalf("instance should be dead, got %v", err)
	}
	// Other instances and the host are fine.
	m2, _ := load(t)
	if n, err := m2.Len("ok", nil); n != 2 || err != nil {
		t.Fatalf("Len = %d, %v", n, err)
	}
}

// TestGoFaultsStillPanic: faults in Go code still go to the Go runtime.
func TestGoFaultsStillPanic(t *testing.T) {
	load(t) // installs the handler
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "nil pointer") {
			t.Fatalf("recovered %v", r)
		}
	}()
	var p *int
	_ = *p
}

func TestBackground(t *testing.T) {
	m, h := load(t)
	if err := m.StartTicker(5); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if n := h.ticks.Load(); n != 0 {
		t.Fatalf("without Background, %d ticks happened outside calls", n)
	}

	m2, h2 := loadOpts(t, &gig.Options{Background: true})
	if err := m2.StartTicker(5); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	n := h2.ticks.Load()
	if n < 10 {
		t.Fatalf("with Background, only %d ticks in 200ms", n)
	}
	// Calls still work while the module's goroutines run.
	if n, err := m2.Len("abc", nil); n != 3 || err != nil {
		t.Fatalf("Len = %d, %v", n, err)
	}
	m2.Close()
	after := h2.ticks.Load()
	time.Sleep(50 * time.Millisecond)
	if h2.ticks.Load() != after {
		t.Fatal("ticks after Close")
	}
	t.Logf("%d ticks in 200ms", n)
}

func TestHeapGrowth(t *testing.T) {
	m, _ := loadOpts(t, &gig.Options{HeapSize: 1 << 20, HeapMax: 128 << 20})
	n, err := m.Hold(64)
	if err != nil || n != 64 {
		t.Fatalf("Hold = %d, %v", n, err)
	}
	st, _ := m.Instance.HeapStats()
	if st.Arena < 64<<20 {
		t.Fatalf("heap didn't grow: %+v", st)
	}
	t.Logf("heap grew to %d MiB", st.Arena>>20)
	// Past HeapMax: out of memory aborts the instance.
	var out bytes.Buffer
	m2, _ := loadOpts(t, &gig.Options{HeapSize: 1 << 20, HeapMax: 16 << 20, Stderr: &out, Stdout: &out})
	if _, err := m2.Hold(64); !errors.Is(err, gig.ErrAborted) || !strings.Contains(out.String(), "out of memory") {
		t.Fatalf("got %v, output %q", err, out.String())
	}
}

func TestPool(t *testing.T) {
	path := kitchenModule(t)
	p, err := gig.NewPool(4, func() (*kitchen.Module, error) { return kitchen.Load(path, &host{}, nil) })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// 8 calls that each sleep 100ms in the module: 4 at a time.
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := p.Do(func(m *kitchen.Module) error {
				_, err := m.Sleepy(1, 100)
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if d := time.Since(start); d < 200*time.Millisecond || d > 600*time.Millisecond {
		t.Fatalf("8 calls of 100ms on 4 instances took %v", d)
	}
	// A crashed instance is replaced.
	for i := 0; i < 4; i++ {
		p.Do(func(m *kitchen.Module) error { _, err := m.Peek(8); return err })
	}
	err = p.Do(func(m *kitchen.Module) error {
		n, err := m.Len("ok", nil)
		if n != 2 {
			t.Errorf("Len = %d", n)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestManyFaults: faults from concurrent goroutines, over and over, leave
// the process (signal masks, signal stacks) healthy.
func TestManyFaults(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				m, err := kitchen.Load(kitchenModule(t), &host{}, nil)
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := m.Peek(uintptr(8 * (i + 1))); !errors.Is(err, gig.ErrFault) {
					t.Errorf("got %v", err)
				}
				m.Close()
			}
		}()
	}
	wg.Wait()
	runtime.GC()
	m, _ := load(t)
	if n, err := m.Sleepy(4, 10); n != 4 || err != nil {
		t.Fatalf("Sleepy = %d, %v", n, err)
	}
}

func TestStackOverflow(t *testing.T) {
	var out bytes.Buffer
	m, _ := loadOpts(t, &gig.Options{Stderr: &out, Stdout: &out})
	if n, err := m.Recurse(100); err != nil || n == 0 {
		t.Fatalf("Recurse(100) = %d, %v", n, err)
	}
	_, err := m.Recurse(1_000_000)
	if !errors.Is(err, gig.ErrAborted) && !errors.Is(err, gig.ErrFault) {
		t.Fatalf("got %v", err)
	}
	t.Logf("%v %q", err, out.String())
}
