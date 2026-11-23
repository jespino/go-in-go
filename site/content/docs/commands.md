---
title: "Command line"
description: "go-in-go build, bindgen and inspect, their flags, and what a module project contains."
weight: 3
---

## A module project

A module project is a directory with:

- `go.mod`: its last path element is the module's name (`module example.com/greeter`
  builds `greeter.gig`).
- Go files of package `main`. Functions marked `//gig:export` and
  `//gig:import` define the module's interface; see
  [Writing modules](/docs/modules/). `main()` is optional: go-in-go adds an
  empty one if it's missing.

Test files (`_test.go`) are ignored, and so are subdirectories: a module is
one package, and go-in-go only copies the files of that directory into the
build. It can import standard and third-party packages (required in
`go.mod`) as long as TinyGo can compile them, but not packages from its own
subdirectories.

## go-in-go build

```sh
go-in-go build [-o out.gig] [-opt level] [-v] [dir]
```

Builds the module in `dir` (default: the current directory).

| Flag | Default | Meaning |
|---|---|---|
| `-o` | `<dir>/<name>.gig` | output file |
| `-opt` | `2` | TinyGo optimization level (`0`, `1`, `2`, `s`, `z`) |
| `-v` | off | print the commands go-in-go runs |

What it does:

1. Copies the project's Go files into a work directory under
   `~/.cache/go-in-go` (or `$GOINGOCACHE`), removing the bodyless
   `//gig:import` declarations.
2. Generates `zz_goingo.go`: a C-callable entry point for each export and
   a body for each import.
3. Compiles with `tinygo build` for go-in-go's own target, into an ELF
   shared object.
4. Embeds the module's manifest (exports, imports and their signatures) in
   a `.goingo` section with `objcopy`.

The output is one self-contained file. Copy it anywhere; it doesn't depend
on the machine that built it, beyond being Linux x86-64.

## go-in-go bindgen

```sh
go-in-go bindgen -pkg name [-o file.go] module.gig
```

Generates typed host bindings for a module, as a Go package named `name`
(written to stdout without `-o`). The generated code imports only
`github.com/jespino/go-in-go/gig`. It contains:

| Identifier | What |
|---|---|
| `Module` | a loaded instance, with a method per export |
| `Imports` | an interface with a method per import (only if the module has imports) |
| `Load(path, imports, opts)` | reads and loads the module file |
| `LoadBytes(data, imports, opts)` | loads it from memory, e.g. with `//go:embed` |
| `(*Module).Close` | unloads the instance |
| `(*Module).GigInstance` | the underlying `*gig.Instance`, e.g. for `gig.Pool` |

Each method returns what the export returns, plus an `error` for failures
of the call itself (the module panicked or crashed, or the instance is
closed). If the export's last result is already an `error`, both kinds
share it.

Rerun `bindgen` whenever a module's exports or imports change. `Load`
checks every signature against the module and refuses a mismatch.

## go-in-go inspect

```sh
go-in-go inspect module.gig
```

Prints a module's name, ABI version, the TinyGo version that built it, and
its exports and imports:

```
module greeter (go-in-go ABI 2, built with TinyGo 0.44.0)
  export Greet(string)(string,error)
  export WordCount(string,int,int)(string)
  import Log(string)()
  import Config(string)(string,bool)
```

Set `GOINGO_JSON=1` to also print the manifest as JSON.
