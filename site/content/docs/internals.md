---
title: "How it works"
description: "The module format, the loader, stack switching, host calls, the two runtimes, and fault containment."
weight: 9
---

```
module *.go ──scan //gig:export, //gig:import──▶ copy + zz_goingo.go (generated glue)
            ──tinygo build (goingo target, overlay TINYGOROOT)──▶ ELF shared object
            ──objcopy --add-section .goingo=manifest.json──▶ module.gig

host:  gig.Load ──▶ map + relocate the ELF (pure Go) ──▶ heap, system stack, context page
                ──▶ resolve the host symbols ──▶ gig_init (package init + main)
```

## The module

A module is an x86-64 ELF shared object, built by TinyGo for go-in-go's own
target (`internal/assets/_files/target.json`): position-independent code,
linked `-Bsymbolic` with no dependencies, TinyGo's conservative garbage
collector and its `tasks` scheduler (goroutines with their own stacks,
switched cooperatively). A module links no libc. It carries a few lines of
`memcpy`/`memset`/`memmove`/`memcmp`/`strlen` instead, and it never makes a
system call or touches thread-local storage.

The runtime layer (`overlay/src/runtime/runtime_goingo.go`, added on top of
a mirror of your TinyGo installation) routes everything the runtime needs
from the outside to the host: output, the clock, sleeping and aborting.
Its whole interface with the host is six symbols, which the loader
provides:

| Symbol | What |
|---|---|
| `gig_yield` | the function the module calls to make a host call |
| `gig_ctx` | the instance's context page |
| `_heap_start`, `_heap_end`, `_heap_max` | the heap: its start, initial end, and how far it can grow |
| `_stack_top` | the top of the module's system stack |

Together with the host call numbers and the way values are passed, this is
the module ABI, versioned as `gig.ABIVersion` and recorded in each
module's manifest. It doesn't involve the Go version of the host at all,
which is why modules work with any.

The manifest (exports, imports, their signatures, the TinyGo version) is
JSON in a `.goingo` ELF section that isn't loaded into memory.

## The generated glue

For each export, go-in-go generates a C-callable function
`gig_export_<Name>(args, rets *uint64)`. It copies the arguments out of the
host's memory (strings and byte slices into the module heap), runs the
function on a module goroutine, and stores the results. For each import,
it generates the body: flatten the arguments into slots, make the host
call, copy the results in.

Goroutines that run exports are **reused**. Starting a goroutine allocates
a 64 KiB stack on the module heap, which would cost about 50 times more
than the rest of the call; parked workers make a call cost about 80 ns
instead of 4 µs.

## The loader

`gig/elf.go` is a minimal dynamic loader in pure Go. It maps the `PT_LOAD`
segments, applies the module's relocations (`R_X86_64_RELATIVE`, `64`,
`GLOB_DAT`, `JUMP_SLOT`), resolving its undefined symbols to the six host
symbols, and sets each page's protection. It also maps, for each instance:

- the heap: `HeapMax` of address space reserved without committing memory,
  with a guard page below it;
- the system stack, with a guard page;
- the context page: where the trampolines save stack pointers and pass call
  information, and where exports' argument and result slots live;
- a scratch area for strings the host passes in, which the module copies
  before running anything else.

## Calls and host calls

`gig/call_amd64.s` has the trampolines, about 80 lines.

- **`enter`** pushes the host's callee-saved registers on the goroutine
  stack, saves the stack pointer in the context page, switches to the
  module's system stack and calls the export's entry point.
- When the module needs the host, it calls **`gig_yield`**. That pushes
  the *module's* callee-saved registers on the module stack, saves its
  stack pointer, records the call's number and slots, restores the host's
  stack pointer and *returns from `enter`*. Back in ordinary Go, the
  loader serves the request (print, sleep, run your import) and calls…
- **`resume`**, which saves the host side again and switches back into the
  module, which returns from `gig_yield` with the answer.

A host call is a coroutine switch, not a callback into Go. Go code only
ever runs on goroutine stacks, in its normal state, which is why no cgo
callback machinery is needed. It also means the host goroutine can move to
another OS thread between a host call and the `resume`: module code never
depends on the thread it runs on.

## Two runtimes side by side

While the module runs, the host goroutine is marked as being in a system
call (`runtime.entersyscall`/`exitsyscall`, which the Go runtime keeps
accessible for this kind of use, see
[go.dev/issue/67401](https://go.dev/issue/67401)). The host's scheduler
hands its processor to other goroutines, and the host's garbage collector
doesn't wait for it: the tests run over a thousand host collections during
a single 300 ms module call.

The two collectors never see each other's memory. The module's
conservative collector scans its own heap, globals and stacks; the host's
scans Go memory. Since values are copied at the boundary, neither side
holds a pointer the other's collector would need to know about.

The module's scheduler runs on its system stack. An export call hands the
function to a worker goroutine and runs the scheduler until that goroutine
finishes; other module goroutines run whenever they're runnable meanwhile.
With `Background`, a host goroutine calls the module's `gig_poll` to run
runnable goroutines between calls, and sleeps until the next sleeper is due.

## Containing failures

- **Panics** end in the runtime's `abort`, which makes an *abort* host call.
  The loader marks the instance dead and never resumes it: its stacks are
  simply abandoned.
- **Faults**: gig installs a handler for `SIGSEGV`, `SIGBUS`, `SIGILL`,
  `SIGFPE` and `SIGTRAP` in front of the Go runtime's. If the faulting
  instruction is inside a loaded module, the handler writes the fault into
  that instance's context page and edits the saved registers so that,
  when the kernel returns from the signal, the thread resumes on the host's
  stack, as if the module had made a host call. Any other fault goes to the
  Go runtime's handler untouched, so your program's own nil dereferences
  still panic normally.
- **Stack overflows** happen on goroutine stacks, which are heap objects:
  an overflowing stack runs down through that instance's heap and hits the
  guard page below it, which faults. The heap it overwrote belongs to the
  instance being discarded.
