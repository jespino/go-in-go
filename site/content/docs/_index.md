---
title: "Documentation"
description: "Everything about building Go modules with go-in-go and loading them into Go programs."
weight: 1
cards: false
---

`go-in-go` builds Go code into **modules**: native x86-64 files (`.gig`)
compiled with TinyGo, which any Go program can load and call, whatever Go
version that program is built with. A module is a plain Go `main` package:
functions marked `//gig:export` can be called by the host, and functions
declared with `//gig:import` are provided by the host. Hosts load modules
with the `gig` package and call them through generated, typed bindings,
without cgo.

> [!WARNING]
> go-in-go is an **experimental** proof of concept. A module is native code
> running inside your process: panics and crashes are contained, but module
> code using `unsafe` can still write anywhere in memory. Only load modules
> you trust, as you would a C library. The API and the module format may
> change without notice.

## Start here

- [Installation](/docs/install/): requirements and installing the `go-in-go` command.
- [Tutorial](/docs/tutorial/): build a module and call it from a Go program in ten minutes.

## Guides

- [Command line](/docs/commands/): `go-in-go build`, `bindgen` and `inspect`.
- [Writing modules](/docs/modules/): exports, imports, types, errors, goroutines and the standard library.
- [Loading modules](/docs/host/): bindings, options, errors, pools and background goroutines.
- [Examples](/docs/examples/): a complete module and host program to learn from.

## Reference

- [API reference](/api/): every exported identifier of the `gig` package.
- [How it works](/docs/internals/): the module format, the loader, stack switching, host calls and fault containment.
- [Limitations](/docs/limitations/): what isn't supported.
- [Development](/docs/development/): building go-in-go itself and running its tests.
