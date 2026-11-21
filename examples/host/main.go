// A Go program loading the greeter module. Build it with any Go version: the
// module is TinyGo code compiled once, and doesn't care.
//
//	go-in-go build examples/greeter
//	go run ./examples/host examples/greeter/greeter.gig
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jespino/go-in-go/examples/host/greeter"
	"github.com/jespino/go-in-go/gig"
)

// host implements greeter.Imports.
type host struct{ config map[string]string }

func (h *host) Log(msg string) { log.Printf("[module] %s", msg) }

func (h *host) Config(key string) (string, bool) {
	v, ok := h.config[key]
	return v, ok
}

func main() {
	path := "examples/greeter/greeter.gig"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	log.SetFlags(0)
	fmt.Printf("host: %s %s/%s, cgo-free loader\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	h := &host{config: map[string]string{"lang": "es"}}
	m, err := greeter.Load(path, h, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer m.Close()

	s, err := m.Greet("gopher")
	fmt.Printf("Greet: %q %v\n", s, err)
	_, err = m.Greet("")
	fmt.Printf("Greet(\"\"): module error: %v\n", err)

	text := strings.Repeat("the quick brown fox jumps over the lazy dog. The dog sleeps! ", 50)
	start := time.Now()
	top, err := m.WordCount(text, 4, 3)
	fmt.Printf("WordCount (4 module goroutines, %v):\n%s", time.Since(start).Round(time.Millisecond), indent(top))
	check(err)

	r, err := m.Reverse([]byte("!og-ni-og morf olleh"))
	fmt.Printf("Reverse: %s %v\n", r, err)

	mean, err := m.Mean(1, 2, 4.5)
	fmt.Printf("Mean: %v %v\n", mean, err)

	// The module's GC runs on its own heap, while the host's GC runs on its own.
	start = time.Now()
	sum, err := m.Churn(2000)
	check(err)
	st, err := m.Instance.HeapStats()
	check(err)
	fmt.Printf("Churn: checksum %d in %v; module heap: %d MiB allocated through a %d MiB arena\n",
		sum, time.Since(start).Round(time.Millisecond), st.TotalAlloc>>20, st.Arena>>20)

	// Many goroutines share one instance: calls are serialized, and the host
	// GC keeps running while they wait or while the module runs.
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := map[string]bool{}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := m.Greet(fmt.Sprint("g", i))
			check(err)
			_ = make([]byte, 1<<20) // host garbage
			mu.Lock()
			results[s] = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	runtime.GC()
	fmt.Printf("50 concurrent Greet calls: %d distinct results\n", len(results))

	// Calls are cheap enough for fine-grained use.
	const n = 100000
	start = time.Now()
	for i := 0; i < n; i++ {
		if _, err := m.Mean(1, 2, 3); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("Mean x %d: %v per call\n", n, time.Since(start)/n)

	// A panic in the module kills the instance, not the host.
	err = m.Crash("nil map")
	fmt.Printf("Crash: %v (aborted: %v)\n", err, errors.Is(err, gig.ErrAborted))
	_, err = m.Greet("again")
	fmt.Printf("Greet after crash: %v\n", err)

	// A fresh instance is independent.
	m2, err := greeter.Load(path, &host{config: map[string]string{}}, nil)
	check(err)
	s, err = m2.Greet("again")
	fmt.Printf("new instance: %q %v\n", s, err)
	m2.Close()
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\n  ") + "\n"
}
