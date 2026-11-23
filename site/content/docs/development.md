---
title: "Development"
description: "Building go-in-go itself, running its tests, and checking that modules work across Go versions."
weight: 11
---

```sh
make            # build ./go-in-go
make test       # go vet + all tests (the integration tests need TinyGo)
make example    # build the greeter module and run the example host
make versions   # build the test module once, run the tests with Go 1.22 to 1.26
make site       # build this website into site/public/
make site-serve # preview it at http://localhost:1313/go-in-go/
```

## Layout

| Path | What |
|---|---|
| `main.go` | the `go-in-go` command |
| `gig/` | the host package: loader (`elf.go`), trampolines (`call_amd64.s`), instances, calls and host calls (`instance.go`), fault containment (`fault_linux_amd64.go`), pools (`pool.go`) |
| `internal/build/` | `go-in-go build` (`scan.go`, `gen.go`, `build.go`) and `bindgen` (`bindgen.go`) |
| `internal/assets/_files/` | the TinyGo target, the runtime overlay and the mini libc |
| `examples/` | the greeter module and its host |
| `test/` | integration tests and their test module |
| `site/` | this website (Hugo) |

The Go files under `internal/assets/_files/` only compile with TinyGo for
the goingo target, so `go vet` and `go test` don't check them: the
integration tests do, by building modules. Editing them invalidates the
cached TinyGo root (`~/.cache/go-in-go/root-<hash>`) automatically.

## Integration tests

`test/integration_test.go` builds `test/testdata/kitchen` with TinyGo and
loads it through the bindings in `test/kitchen/`. When the module's
exports or imports change, regenerate the bindings:

```sh
./go-in-go build -o /tmp/kitchen.gig test/testdata/kitchen
./go-in-go bindgen -pkg kitchen -o test/kitchen/kitchen_gig.go /tmp/kitchen.gig
```

(`TestBindingsUpToDate` fails if they're stale.)

To run the tests against a prebuilt module instead of building one, set
`GIG_TEST_MODULE`. That's how `make versions` checks that one module works
with every Go version:

```sh
./go-in-go build -o /tmp/kitchen.gig test/testdata/kitchen
GIG_TEST_MODULE=/tmp/kitchen.gig GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 go test ./test
```

The tests also run under the race detector (`go test -race ./test`).

## Changing the ABI

Anything the module and the loader must agree on (host call numbers, the
context page layout, the symbols the loader provides, how values are
encoded) is the module ABI. Change both sides together and bump
`gig.ABIVersion`: loaders refuse modules built for another version.
