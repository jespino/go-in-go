---
title: "go-in-go"
heroTitle: 'Go modules for <span class="accent">any Go</span> version'
lead: "go-in-go builds Go code once, with TinyGo, into a native module that any Go program can load and call, whatever Go version that program is built with. No cgo, no `dlopen`, no libc: the loader is plain Go and a few lines of assembly."
warning: "go-in-go is an experimental proof of concept. A module is native code running inside your process: panics and crashes are contained, but treat a module like a C library you link in, not like a sandbox. The API and the module format may change without notice."
actions:
  - label: "Get started"
    url: "/docs/tutorial/"
    primary: true
  - label: "Documentation"
    url: "/docs/"
  - label: "GitHub"
    url: "https://github.com/jespino/go-in-go"
features:
  - title: "Build once, load anywhere"
    text: "One `.gig` file passes the same test suite in hosts built with Go 1.22, 1.23, 1.24, 1.25 and 1.26. The module never depends on the host's Go version."
  - title: "Just Go, on both sides"
    text: "Modules are plain Go: `fmt`, maps, `sync`, goroutines, channels, `time.Sleep`. Hosts call them through generated, typed bindings."
  - title: "No cgo"
    text: "A pure-Go ELF loader maps and relocates the module, and about 80 lines of assembly switch stacks. Hosts build with `CGO_ENABLED=0`."
  - title: "Two runtimes, side by side"
    text: "The module brings its own garbage collector, heap and scheduler. While it runs, the host's scheduler and GC keep going."
  - title: "Contained failures"
    text: "A panic, a segfault or a stack overflow in a module kills that instance, not your program: the call returns an error."
  - title: "Fast calls"
    text: "A call into a module costs about 80 ns, including both stack switches. Pools of instances run calls in parallel."
---

## Hello, world

A module is a Go `main` package. Mark what the host can call, and declare
what the module needs from the host:

```go
package main

import (
	"errors"
	"fmt"
)

// Log writes a message through the host's logger.
//
//gig:import
func Log(msg string)

// Greet returns a greeting for name.
//
//gig:export
func Greet(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty name")
	}
	Log("greeting " + name)
	return fmt.Sprintf("Hello, %s!", name), nil
}
```

```sh
go-in-go build ./greeter                                    # → greeter/greeter.gig
go-in-go bindgen -pkg greeter -o host/greeter/greeter_gig.go greeter/greeter.gig
```

Any Go program can then load it:

```go
type host struct{}

func (host) Log(msg string) { log.Println("module:", msg) }

func main() {
	m, err := greeter.Load("greeter.gig", host{}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer m.Close()
	s, err := m.Greet("gopher") // "Hello, gopher!"
	fmt.Println(s, err)
}
```

The [tutorial](/docs/tutorial/) walks through it step by step.

## Why not `plugin`?

Go's `plugin` package only works when the plugin and the program are built
with exactly the same Go version and the same versions of every shared
dependency, because both use one runtime and one type system. go-in-go
shares nothing: each side keeps its own runtime and memory. They only meet
at the C calling convention and a tiny, versioned interface, so a module
outlives the Go version it was built next to.

## A sibling project

go-in-go uses the same approach as [gokm](https://jespino.github.io/gokm/)
(Linux kernel modules in Go) and [gopgext](https://jespino.github.io/gopgext/)
(PostgreSQL extensions in Go): a custom TinyGo target and runtime layer for
the host, and generated glue. This time the host is a Go program.
