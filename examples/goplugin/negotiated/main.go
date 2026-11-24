// go-plugin's "negotiated" example with go-in-go: a key/value CLI and a
// plugin that agree on a protocol version when the plugin is loaded.
//
//	go-in-go build examples/goplugin/negotiated/plugin
//	KV_PROTO=3 go run ./examples/goplugin/negotiated put hello world
//	KV_PROTO=2 go run ./examples/goplugin/negotiated get hello
//	world
//
//	Written from plugin version 3
//	Read by plugin version 2
//
// KV_PROTO is the comma-separated list of versions this host supports (go-plugin's
// example maps KV_PROTO=netrpc to version 2 and KV_PROTO=grpc to version 3).
// Set it to "2,3" and the plugin picks 3; leave it empty and the host
// doesn't negotiate, so the plugin falls back to its oldest version, 2.
package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/jespino/go-in-go/examples/goplugin/hostfs"
	"github.com/jespino/go-in-go/examples/goplugin/negotiated/shared"
)

func run() error {
	path := os.Getenv("KV_PLUGIN")
	if path == "" {
		path = "examples/goplugin/negotiated/plugin/kv.gig"
	}
	kv, err := shared.Load(path, hostfs.Dir("."), nil)
	if err != nil {
		return err
	}
	defer kv.Close()

	supported := os.Getenv("KV_PROTO")
	version, err := kv.Negotiate(supported)
	if err != nil {
		return err
	}
	// Don't take the plugin's word for it.
	if supported != "" && !slices.Contains(strings.Split(supported, ","), strconv.Itoa(version)) {
		return fmt.Errorf("plugin chose protocol version %d, which this host doesn't support", version)
	}
	fmt.Fprintf(os.Stderr, "using protocol version %d\n", version)

	args := os.Args[1:]
	switch {
	case len(args) == 2 && args[0] == "get":
		result, err := kv.Get(args[1])
		if err != nil {
			return err
		}
		fmt.Println(string(result))
	case len(args) == 3 && args[0] == "put":
		return kv.Put(args[1], []byte(args[2]))
	default:
		return fmt.Errorf("usage: kv get KEY | kv put KEY VALUE")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
