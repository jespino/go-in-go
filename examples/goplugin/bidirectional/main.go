// go-plugin's "bidirectional" example with go-in-go: a key/counter CLI whose
// storage is a plugin, and the plugin calls back into the host to add.
//
//	go-in-go build examples/goplugin/bidirectional/plugin
//	export COUNTER_PLUGIN=examples/goplugin/bidirectional/plugin/counter.gig
//	go run ./examples/goplugin/bidirectional put hello 1
//	go run ./examples/goplugin/bidirectional put hello 1
//	go run ./examples/goplugin/bidirectional get hello
//	2
//
// go-plugin needs its gRPC broker to let the plugin call the host: the host
// serves the AddHelper on a new connection and passes its ID to the plugin.
// Here the host's side is the Imports interface of the bindings: host
// implements Sum, and the module calls it like a local function.
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/jespino/go-in-go/examples/goplugin/bidirectional/shared"
	"github.com/jespino/go-in-go/examples/goplugin/hostfs"
)

// host implements shared.Imports: the add helper, plus file access.
type host struct {
	hostfs.Dir
	sums int
}

// Sum is the add helper the plugin calls back into.
func (h *host) Sum(a, b int64) (int64, error) {
	h.sums++
	return a + b, nil
}

func run() error {
	path := os.Getenv("COUNTER_PLUGIN")
	if path == "" {
		return fmt.Errorf("set COUNTER_PLUGIN to a plugin module (.gig)")
	}
	h := &host{Dir: "."}
	counter, err := shared.Load(path, h, nil)
	if err != nil {
		return err
	}
	defer counter.Close()

	args := os.Args[1:]
	switch {
	case len(args) == 2 && args[0] == "get":
		result, err := counter.Get(args[1])
		if err != nil {
			return err
		}
		fmt.Println(result)
	case len(args) == 3 && args[0] == "put":
		i, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil {
			return err
		}
		if err := counter.Put(args[1], i); err != nil {
			return err
		}
		if h.sums != 1 {
			return fmt.Errorf("the plugin called Sum %d times, want 1", h.sums)
		}
	default:
		return fmt.Errorf("usage: counter get KEY | counter put KEY N")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
