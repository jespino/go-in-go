---
title: "Examples"
description: "A complete module and the host program that loads it, and the test modules that cover the edge cases."
weight: 8
---

## greeter

[`examples/greeter`](https://github.com/jespino/go-in-go/tree/main/examples/greeter)
is a module that shows most of what a module can do:

| Export | Shows |
|---|---|
| `Greet(name) (string, error)` | strings, an `error` result, global state, calling host functions |
| `WordCount(text, workers, n) string` | a pool of module goroutines, channels, `sync.Mutex`, `time.Sleep`, maps, `sort` |
| `Churn(rounds) uint64` | heavy allocation, collected by the module's own GC |
| `Reverse(b []byte) []byte` | byte slices in and out |
| `Mean(a, b, c float64) float64` | floats |
| `Crash(msg string)` | a panic, contained to the instance |

It imports two host functions: `Log(msg string)` and
`Config(key string) (string, bool)`.

[`examples/host`](https://github.com/jespino/go-in-go/tree/main/examples/host)
is a Go program that loads it through generated bindings, calls every
export, calls one instance from 50 goroutines at once, measures the cost
of a call, survives the module's panic and loads a fresh instance. Run it
from the repository:

```sh
make example
```

```
host: go1.26.8 linux/amd64, cgo-free loader
[module] greeter module initialized
Greet: "Hola, gopher! (greeting #1 from TinyGo inside Go)" <nil>
Greet(""): module error: empty name
WordCount (4 module goroutines, 20ms):
  the=150
  dog=100
  brown=50
Reverse: hello from go-in-go! <nil>
Mean: 2.5 <nil>
Churn: checksum 876720000 in 61ms; module heap: 644 MiB allocated through a 3 MiB arena
50 concurrent Greet calls: 50 distinct results
Mean x 100000: 77ns per call
panic: assignment to entry in nil map
Crash: gig: module aborted (panic or fatal error) (aborted: true)
Greet after crash: gig: module aborted (panic or fatal error)
[module] greeter module initialized
new instance: "Hello, again! (greeting #1 from TinyGo inside Go)" <nil>
```

Build the host with another Go version, and the same `greeter.gig` still
works:

```sh
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 go run ./examples/host
```

## go-plugin's examples

[`examples/goplugin`](https://github.com/jespino/go-in-go/tree/main/examples/goplugin)
rebuilds the five examples of
[hashicorp/go-plugin](https://github.com/hashicorp/go-plugin/tree/main/examples)
with go-in-go. The plugins are modules loaded into the host process, not
subprocesses reached over RPC:

| go-plugin example | Shows |
|---|---|
| `basic` | loading a plugin and calling it; the plugin logs through the host's `slog` logger |
| `grpc` (here `kv`) | a key/value CLI with its storage in a plugin picked at run time, and two implementations behind the same bindings |
| `bidirectional` | the plugin calling back into the host (an import) in the middle of a call |
| `negotiated` | one plugin with protocol versions 2 and 3, agreeing on one with the host |
| `streaming` | chunks streamed both ways: an export per chunk into the plugin, an import per chunk back |

Plugins can't open files themselves, so the hosts give them file access
limited to one directory through imports. The examples'
[README](https://github.com/jespino/go-in-go/tree/main/examples/goplugin)
maps each go-plugin concept to its go-in-go equivalent. Build and run them
all, with their output checked:

```sh
make goplugin
```

## The test module

[`test/testdata/kitchen`](https://github.com/jespino/go-in-go/tree/main/test/testdata/kitchen)
is the module the integration tests load. It covers the edge cases:
every type in both directions (through an import that echoes them back),
reentrant calls, panics in host functions, deadlocks, values of several
megabytes, a module keeping data alive across its own collections, a
segfault, a stack overflow, heap growth up to its limit, a background
ticker, and pools. It's a good reference for how each situation looks
from the host.
