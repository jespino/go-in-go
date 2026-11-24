// go-plugin's "grpc" example with go-in-go: a key/value CLI whose storage is
// a plugin, chosen at run time.
//
//	go-in-go build examples/goplugin/kv/plugin-plain
//	go-in-go build examples/goplugin/kv/plugin-base64
//	export KV_PLUGIN=examples/goplugin/kv/plugin-plain/kv-plain.gig
//	go run ./examples/goplugin/kv put hello world
//	go run ./examples/goplugin/kv get hello
//
// In go-plugin, KV_PLUGIN names an executable and KV_PROTO picks net/rpc or
// gRPC to talk to it. Here KV_PLUGIN names a module file, and there's no
// protocol to pick: the host calls the module's functions directly. Any
// module exporting Put and Get and importing ReadFile and WriteFile with the
// signatures in shared/ can be loaded, which is what makes it a plugin
// interface: shared/ was generated from plugin-plain, and loads plugin-base64
// just as well.
package main

import (
	"fmt"
	"os"

	"github.com/jespino/go-in-go/examples/goplugin/hostfs"
	"github.com/jespino/go-in-go/examples/goplugin/kv/shared"
)

func run() error {
	path := os.Getenv("KV_PLUGIN")
	if path == "" {
		return fmt.Errorf("set KV_PLUGIN to a plugin module (.gig)")
	}
	// The plugin's files go to the current directory, as in go-plugin.
	kv, err := shared.Load(path, hostfs.Dir("."), nil)
	if err != nil {
		return err
	}
	defer kv.Close()

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
		fmt.Printf("error: %+v\n", err)
		os.Exit(1)
	}
}
