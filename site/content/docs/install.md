---
title: "Installation"
description: "What you need to build modules with go-in-go, what you need to load them, and how to install the command."
weight: 1
---

Building a module and loading one have different requirements. Only the
machine that builds modules needs TinyGo.

## To build modules

- Linux x86-64
- Go, to install the `go-in-go` command
- [TinyGo](https://tinygo.org/getting-started/install/) (tested with
  TinyGo 0.44.0)
- binutils' `objcopy`, which embeds the module's manifest

Check that the tools can be found:

```sh
go version
tinygo version
objcopy --version
```

Then install the command:

```sh
go install github.com/jespino/go-in-go@latest
```

Or, from a clone of the repository:

```sh
make          # builds ./go-in-go
```

Make sure `$(go env GOPATH)/bin` is in your `PATH`, then run `go-in-go`
with no arguments to see its commands.

The first build of a module takes a little longer: go-in-go prepares its
TinyGo files (a target and a runtime layer on top of your TinyGo
installation) in `~/.cache/go-in-go`. Set `$GOINGOCACHE` to put them
somewhere else. They're rebuilt automatically when go-in-go or TinyGo
change.

## To load modules

A program that loads modules only needs the `gig` package:

```sh
go get github.com/jespino/go-in-go/gig
```

- Linux x86-64
- Go 1.22 or later, any version: the module doesn't depend on it
- No cgo: `CGO_ENABLED=0` and fully static binaries work
- No TinyGo, no binutils, no C compiler
