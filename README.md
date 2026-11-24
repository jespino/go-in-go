<p align="center">
  <img src="assets/gopher-gopher.png" alt="The Go gopher in a Go gopher costume" width="240">
</p>

# go-in-go — Go modules for any Go version

> [!WARNING]
> Proof of concept. Module code is native code running in your process:
> treat a module like a C library you link in, not like a sandbox.

`go-in-go` compiles Go code with TinyGo into a **module**: a native x86-64
ELF file. Any Go program can then load the module and call into it, **whatever
Go version that program is built with**. You build the module once and load it
from a host built with Go 1.22, 1.26 or a future release. The host needs no
cgo, no `dlopen` and no libc.

**Documentation: <https://jespino.github.io/go-in-go/>**. New to go-in-go? The
[tutorial](https://jespino.github.io/go-in-go/docs/tutorial/) walks you through building a module and
calling it from Go programs built with different Go versions.

```sh
go-in-go build examples/greeter                     # → examples/greeter/greeter.gig
go-in-go bindgen -pkg greeter -o examples/host/greeter/greeter_gig.go examples/greeter/greeter.gig
go run ./examples/host                              # any Go ≥ 1.22, CGO_ENABLED=0 is fine
```

The module is plain Go:

```go
package main

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

The host uses the generated typed bindings:

```go
m, err := greeter.Load("greeter.gig", myImports, nil) // myImports implements greeter.Imports
s, err := m.Greet("gopher")
```

## Why it's version-independent

Go's `plugin` package fails as soon as the host and the plugin differ in Go
version or in any shared dependency, because both use one runtime and one
type system. go-in-go shares **nothing** across the boundary:

- **Each side has its own runtime.** The module carries TinyGo's runtime: its
  own garbage collector over its own heap, and its own goroutine scheduler.
  The host's gc runtime never sees the module's memory or goroutines, and the
  module never sees the host's.
- **The boundary is the C calling convention plus a tiny, versioned ABI.**
  Values cross as 64-bit slots (strings and slices as pointer+length) and are
  always copied. The module expects six symbols from the host: `gig_yield`,
  `gig_ctx`, `_heap_start`, `_heap_end`, `_heap_max` and `_stack_top`. That set is the
  contract, versioned as `gig.ABIVersion`. It's independent of the Go version.

## How it works

```
module *.go ──scan //gig:export, //gig:import──▶ + generated glue (zz_goingo.go)
            ──tinygo build, goingo target──▶ freestanding ELF .so (PIC, no libc, no TLS, no syscalls)
            ──objcopy──▶ + .goingo section (JSON manifest) = module.gig

host:  gig.Load ─▶ mmap + relocate the ELF (pure Go) ─▶ map heap arena, system stack, context page
                ─▶ resolve the 6 host symbols ─▶ run gig_init (package init + main)
```

It's the same approach as [gokm](https://github.com/jespino/gokm) (Linux
kernel modules in Go) and [gopgext](https://github.com/jespino/gopgext): a
custom TinyGo target, a runtime layer for the host, and generated glue. Here
the "host" is a Go program.

- **TinyGo target** (`internal/assets/_files/target.json`): x86-64, PIC,
  linked as a shared object with `-Bsymbolic`, conservative GC, `tasks`
  scheduler, and a 30-line libc (`memcpy` & co.).
- **Runtime layer** (`internal/assets/_files/overlay/src/runtime/runtime_goingo.go`):
  output, time, sleep and abort all go through *host calls*. The module never
  makes a system call itself.
- **Loader** (`gig/elf.go`): maps the `PT_LOAD` segments, applies the
  `RELATIVE`/`GLOB_DAT`/`JUMP_SLOT` relocations and sets page protections.
- **Trampolines** (`gig/call_amd64.s`): about 80 lines of assembly.
  - `enter` saves the Go side's callee-saved registers and switches to the
    module's system stack.
  - When the module needs the host, it calls `gig_yield`. That saves the
    module's registers and *returns* from `enter` back into Go, so a host call
    is a coroutine switch, not a callback into Go. Go code therefore only ever
    runs on goroutine stacks, which is why no cgo callback machinery is needed.
  - `resume` switches back into the module with the answer.
- **The host goroutine is "in a syscall" while the module runs**
  (`runtime.entersyscall`/`exitsyscall`, both kept linkname-accessible by the
  runtime, go.dev/issue/67401). The host scheduler and GC carry on without
  waiting for it: the tests force over 1,500 host GCs during one 300 ms module
  call.
- **Exported calls run on module goroutines** (reused workers), so module
  code can block on channels, mutexes and `time.Sleep`. The module's
  scheduler runs until that goroutine finishes. `time.Sleep` becomes a host
  call to `time.Sleep`, which parks the host goroutine, not the OS thread.
- A module **panic** marks that instance dead and the call returns
  `gig.ErrAborted`. The host is unaffected, and you can load a new instance.
- A module **crash** (segfault, bus error, illegal instruction, stack
  overflow) is contained the same way, returning a `*gig.FaultError` (matches
  `gig.ErrFault`) such as `segmentation fault at module+0x7e6a
  (main.Peek+0xa), address 0x18`.
  - How: gig installs a handler for SIGSEGV/SIGBUS/SIGILL/SIGFPE/SIGTRAP in
    front of the Go runtime's. If the faulting PC is inside a loaded module,
    the handler edits the signal context so the thread resumes on the Go
    stack, as if the module had made a host call. Any other fault goes to Go's
    handler unchanged, so nil dereferences in your Go code still panic
    normally.
  - The module heap has a guard page below it, so an overflowing goroutine
    stack (a heap object) faults there before it can reach other memory.

## Instances, pools and background goroutines

- **Heap**: each instance has its own heap. It starts at `Options.HeapSize`
  (default 4 MiB) and grows by about a third at a time, up to
  `Options.HeapMax` (default 1 GiB). The whole range is reserved as address
  space up front, but memory is only committed as it's used. Going past the
  maximum aborts that instance with "out of memory".
- **Parallelism**: an instance runs one call at a time. `gig.Pool` loads
  several instances and runs calls on whichever is idle, and it replaces
  instances that died:

  ```go
  p, _ := gig.NewPool(runtime.NumCPU(), func() (*greeter.Module, error) {
  	return greeter.Load("greeter.gig", host, nil)
  })
  err := p.Do(func(m *greeter.Module) error { s, err = m.Greet("gopher"); return err })
  ```

- **Background goroutines**: by default, module goroutines only run while the
  host is inside a call. With `Options.Background`, a host goroutine also runs
  them between calls, whenever one is runnable or a sleeper is due (via the
  module's `gig_poll`). A module can then keep timers, tickers and workers
  going. Scheduling is cooperative: a module goroutine that spins without
  blocking holds the instance.

## Supported types

`bool`, `int*`, `uint*`, `uintptr`, `float32`, `float64`, `string`, `[]byte`,
and `error` as the last result. They work in both directions: as arguments and
results of exports, and of imports. Values are copied: a module never keeps a
pointer into host memory, and the host never keeps one into module memory.

## Numbers

These figures are from this machine (Go 1.26, TinyGo 0.44):

- **Call overhead**: about 75–100 ns per export call, including the two stack
  switches, `entersyscall`/`exitsyscall` and handing the call to a module
  goroutine. A call without the goroutine handoff costs about 40 ns.
- **Version check**: one `kitchen.gig` passes the whole test suite on hosts
  built with Go 1.22.12, 1.23.12, 1.24.13, 1.25.9 and 1.26.8, with
  `CGO_ENABLED=0` (`make versions`).

## Limitations

- **Platform**: Linux x86-64 only. The trampolines and the loader are
  arch-specific; arm64 would need its own `call_arm64.s` and relocation types.
- **Not a sandbox**: panics and faults are contained, but module code can
  still read or write any memory of the process through `unsafe`. A stray
  write that doesn't fault isn't detected. Only load modules you trust, as
  you would a C library.
- **Signals**: fault containment assumes the Go runtime's handlers are the
  ones installed. A program that installs its own SIGSEGV handler (via cgo)
  after loading a module bypasses it.
- **One thread per instance**: calls into one instance are serialized, and
  module goroutines never run in parallel with each other. Use `gig.Pool`
  for parallelism.
- **No reentrancy**: a host function can't call back into the instance that
  called it (`gig.ErrReentrant`). Supporting it would need changes to how
  TinyGo's GC scans stacks.
- **Profiling**: in the host's CPU profiles, time spent in a module shows up
  as `gig.enterModule`.
- **Stdlib coverage**: the module gets whatever TinyGo supports for a
  baremetal-like target. That means `fmt`, `strings`, `sort`, `sync`, `time`,
  maps and so on, but no `os` files and no `net`.
- **Quirk**: TinyGo's `NumGC` undercounts collections triggered by
  allocation, so use `HeapStats().TotalAlloc` to see GC activity.

## Layout

| Path | What |
|---|---|
| `gig/` | the host package: loader, trampolines, `Instance`, `Frame`, `HostCall` |
| `internal/build/` | `go-in-go build` (scan, glue generation, TinyGo, manifest) and `bindgen` |
| `internal/assets/_files/` | TinyGo target, runtime overlay, mini libc |
| `examples/greeter/` | an example module |
| `examples/host/` | a host program using it, with generated bindings |
| `examples/goplugin/` | hashicorp/go-plugin's examples rebuilt with go-in-go (`make goplugin`) |
| `site/` | the website (Hugo): `make site`, `make site-serve` |
| `test/` | integration tests (all types both ways, panics, faults, stack overflow, reentrancy, GC on both sides, heap growth, background goroutines, pools, versions) |

Requirements to build modules: TinyGo (tested with 0.44.0) and binutils'
`objcopy`. Loading modules only needs the `gig` package.

## Mascot

The go-in-go mascot is the Go gopher dressed as a Go gopher. It's based on
the Go gopher designed by [Renée French](https://reneefrench.blogspot.com/),
licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).

## License

go-in-go is released under the [MIT License](LICENSE).
