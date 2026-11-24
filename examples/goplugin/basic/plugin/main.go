// The greeter plugin of go-plugin's "basic" example, as a go-in-go module.
//
// In go-plugin, the plugin is a separate executable that serves a Greeter
// over net/rpc and logs JSON to stderr for the host to pick up. Here it's a
// module loaded into the host process: Greet is an export the host calls
// directly, and logging is an import, so log lines go straight into the
// host's logger, attributes included.
package main

import "strings"

// Log emits a structured log line through the host's logger. attrs holds
// key/value pairs, each element separated by a NUL byte.
//
//gig:import
func Log(level int, msg string, attrs string)

// levelDebug is slog.LevelDebug.
const levelDebug = -4

func debug(msg string, kv ...string) { Log(levelDebug, msg, strings.Join(kv, "\x00")) }

func init() {
	debug("message from plugin", "foo", "bar")
}

// Greet returns a greeting.
//
//gig:export
func Greet() string {
	debug("message from GreeterHello.Greet")
	return "Hello!"
}
