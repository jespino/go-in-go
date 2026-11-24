// go-plugin's "basic" example with go-in-go: the host loads the greeter
// plugin and calls Greet.
//
//	go-in-go build examples/goplugin/basic/plugin
//	go run ./examples/goplugin/basic
//
// go-plugin starts the plugin as a subprocess, checks a handshake (protocol
// version and magic cookie), and talks to it over net/rpc. Here the plugin
// is loaded into this process: Load checks that the module exports Greet and
// imports Log with exactly the signatures the bindings were generated from,
// which is what the handshake protects against, and Greet is a function call.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/jespino/go-in-go/examples/goplugin/basic/shared"
)

// host implements shared.Imports: the plugin logs through it.
type host struct{ logger *slog.Logger }

func (h host) Log(level int, msg string, attrs string) {
	var args []any
	if attrs != "" {
		for _, a := range strings.Split(attrs, "\x00") {
			args = append(args, a)
		}
	}
	h.logger.Log(context.Background(), slog.Level(level), msg, args...)
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With("plugin", "greeter")

	path := "examples/goplugin/basic/plugin/greeter.gig"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	// Equivalent of plugin.NewClient + client.Client + Dispense("greeter").
	greeter, err := shared.Load(path, host{logger}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer greeter.Close() // equivalent of client.Kill

	msg, err := greeter.Greet()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(msg)
}
