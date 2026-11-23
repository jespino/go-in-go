---
title: "Writing modules"
description: "Exports and imports, the types that cross the boundary, errors, global state, goroutines and the standard library."
weight: 4
---

A module is a Go `main` package, compiled with TinyGo. It talks to the host
through two kinds of functions: **exports**, which the host calls, and
**imports**, which the host provides.

## Exports

A function marked `//gig:export` can be called by the host:

```go
// WordCount returns the number of words in text.
//
//gig:export
func WordCount(text string) int {
	return len(strings.Fields(text))
}
```

- It must be a top-level, exported (upper-case) function with a body. No
  methods and no generic functions.
- Its doc comment, except the pragma line, ends up on the generated
  binding method.
- Every call runs on a goroutine of the module, so the function can block:
  wait on channels, lock mutexes, `time.Sleep`. The call returns when that
  goroutine finishes.

## Imports

A function declared with `//gig:import` and **no body** is provided by the
host:

```go
// Config looks up a host configuration value.
//
//gig:import
func Config(key string) (string, bool)
```

The module calls it like any other function. It's another top-level,
exported function, and the same types apply. On the host side, it becomes
a method of the bindings' `Imports` interface. Imports can be called from
anywhere in the module: exports, package initializers, `main`, and
goroutines.

## Types

Parameters and results of exports and imports can be:

| Go type | Notes |
|---|---|
| `bool` | |
| `int`, `int8`, `int16`, `int32`, `int64` | `int` is 64 bits on both sides |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64`, `uintptr` | `byte` and `rune` work too |
| `float32`, `float64` | including NaN and infinities |
| `string` | copied |
| `[]byte` | copied; `nil` and empty both arrive as `nil` |
| `error` | only as the **last result**; arrives as an error with the same message |

Values are always **copied** across the boundary, never shared: a module
never keeps a pointer into the host's memory, and the host never keeps one
into the module's. Changing a `[]byte` argument in the module doesn't
change the host's slice; return it instead.

Structs, maps, slices of other types, pointers and function values can't
cross. Encode them (e.g. as JSON in a `string` or `[]byte`), or split them
into several parameters. A signature can use up to 64 slots for its
parameters and 32 for its results (`string`, `[]byte` and `error` take
two, everything else one).

## Errors and panics

- An `error` result is the normal way to report failures. Only its message
  crosses: the host gets an error with the same text, not the same type,
  so compare messages, not with `errors.Is`.
- A **panic** that isn't recovered inside the module prints its message to
  the host's stderr, and the call returns `gig.ErrAborted`. The instance
  is dead from then on: every later call returns the same error. The host
  keeps running and can load a new instance.
- A **crash** (an invalid `unsafe` pointer, a stack overflow) ends the
  same way, with a `*gig.FaultError`. See
  [Loading modules](/docs/host/#errors).

## Global state, init and main

Each loaded instance has its own globals, heap and goroutines. Package
initializers and then `main()` run once, while the host loads the
instance; `Load` returns when `main` returns. Globals keep their values
between calls, and two instances of the same module don't share anything.

## Goroutines

Goroutines, channels, `sync.Mutex`, `sync.WaitGroup`, `select` and
`time.Sleep` work. They're scheduled cooperatively by TinyGo's scheduler,
on one thread per instance: goroutines interleave when they block, and
never run in parallel.

By default, the module's goroutines only run while the host is inside a
call. A goroutine started by an export keeps going during later calls, but
sits still in between. Load the instance with `Background: true` to keep
them running between calls; see
[background goroutines](/docs/host/#background-goroutines).

An export that blocks forever (e.g. on a channel nobody sends to, with no
sleeping goroutine to wake it) is a deadlock: the module panics and the
call returns `gig.ErrAborted`.

`time.Sleep` in a module parks the calling host goroutine, not a thread:
the host gets the time through the host call, and sleeps with its own
`time.Sleep`.

## The standard library

Modules get the standard library that TinyGo supports for a baremetal-like
target: `fmt`, `strings`, `strconv`, `bytes`, `sort`, `slices`, `maps`,
`errors`, `math`, `unicode`, `encoding/binary`, `encoding/hex`,
`encoding/base64`, `sync`, `time`, maps, generics, `defer`/`recover` and
more. Packages that lean on reflection, like `encoding/json`, only partly
work under TinyGo; check
[TinyGo's package support](https://tinygo.org/docs/reference/lang-support/stdlib/).

There's no operating system inside a module. It can't open files, sockets
or processes, read environment variables or command-line arguments. That's
deliberate: anything the module needs from the outside world comes through
an import, which the host controls. `fmt.Println` and `println` work; the
output goes to the host's stdout (see `gig.Options.Stdout`).

## Memory

Each instance has its own garbage-collected heap. It starts small
(`Options.HeapSize`, 4 MiB by default) and grows as needed up to
`Options.HeapMax` (1 GiB by default). A module that needs more than the
maximum stops with "out of memory", which aborts the instance. The host's
garbage collector never sees the module's memory, and the module's never
sees the host's.
