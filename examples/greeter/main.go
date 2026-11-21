// A go-in-go module: plain Go, compiled once with TinyGo, loadable by any Go
// host. Functions marked //gig:export can be called by the host; functions
// declared (without a body) with //gig:import are provided by the host.
package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Log writes a message through the host's logger.
//
//gig:import
func Log(msg string)

// Config looks up a host configuration value.
//
//gig:import
func Config(key string) (string, bool)

var greetings int

func init() {
	Log("greeter module initialized")
}

// Greet returns a greeting for name in the configured language.
//
//gig:export
func Greet(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty name")
	}
	greetings++
	hello := "Hello"
	if lang, ok := Config("lang"); ok && lang == "es" {
		hello = "Hola"
	}
	return fmt.Sprintf("%s, %s! (greeting #%d from TinyGo inside Go)", hello, name, greetings), nil
}

// WordCount counts words in text with a pool of goroutines and returns the
// top n words as "word=count" lines.
//
//gig:export
func WordCount(text string, workers int, n int) string {
	words := strings.Fields(strings.ToLower(text))
	chunks := make(chan []string)
	var mu sync.Mutex
	counts := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := map[string]int{}
			for c := range chunks {
				for _, w := range c {
					local[strings.Trim(w, ".,;:!?\"'")]++
				}
				time.Sleep(time.Millisecond) // yields to the other workers
			}
			mu.Lock()
			for w, c := range local {
				counts[w] += c
			}
			mu.Unlock()
		}()
	}
	for i := 0; i < len(words); i += 8 {
		chunks <- words[i:min(i+8, len(words))]
	}
	close(chunks)
	wg.Wait()

	keys := make([]string, 0, len(counts))
	for w := range counts {
		keys = append(keys, w)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	var sb strings.Builder
	for _, k := range keys[:min(n, len(keys))] {
		fmt.Fprintf(&sb, "%s=%d\n", k, counts[k])
	}
	return sb.String()
}

// Churn allocates and drops rounds*1000 small objects, to exercise the
// module's own garbage collector, and returns a checksum.
//
//gig:export
func Churn(rounds int) uint64 {
	var sum uint64
	for r := 0; r < rounds; r++ {
		list := make([][]byte, 0, 1000)
		for i := 0; i < 1000; i++ {
			b := make([]byte, 64+i%512)
			b[0] = byte(i)
			list = append(list, b)
		}
		for _, b := range list {
			sum += uint64(b[0]) + uint64(len(b))
		}
	}
	return sum
}

// Reverse reverses a byte slice (the module gets and returns a copy).
//
//gig:export
func Reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		out[len(b)-1-i] = c
	}
	return out
}

// Mean returns the mean of three numbers (float math in the module).
//
//gig:export
func Mean(a, b, c float64) float64 { return (a + b + c) / 3 }

// Crash panics inside the module.
//
//gig:export
func Crash(msg string) {
	var m map[string]int
	if msg == "nil map" {
		m["boom"] = 1
	}
	panic(msg)
}
