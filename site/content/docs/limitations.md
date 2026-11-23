---
title: "Limitations"
description: "What go-in-go doesn't support (yet)."
weight: 10
---

- **Linux x86-64 only.** The trampolines, the fault handler and the
  loader's relocations are specific to it. Another architecture needs its
  own versions of them, and other systems a different executable format
  and signal handling.
- **Not a sandbox.** Panics and faults are contained, but module code using
  `unsafe` can read or write any memory of the process, and a stray write
  that doesn't fault goes unnoticed. Load only modules you trust, as you
  would a C library.
- **One thread per instance.** Calls into an instance are serialized, and a
  module's goroutines never run in parallel with each other. Use
  `gig.Pool` for parallel calls.
- **No reentrancy.** A host function can't call back into the instance that
  called it (`gig.ErrReentrant`). Supporting it would need changes to how
  TinyGo's collector scans stacks.
- **Simple types only.** Exports and imports take and return numbers,
  `bool`, `string`, `[]byte` and `error`. No structs, maps, pointers,
  function values or channels; values are always copied.
- **No operating system inside the module.** No files, sockets, processes,
  environment or command-line arguments: a module reaches the outside
  world through its imports.
- **TinyGo's standard library.** Modules get what TinyGo supports;
  packages that rely heavily on reflection only partly work.
- **Cooperative scheduling.** A module goroutine that never blocks holds
  its instance until it does.
- **Fault handlers.** Fault containment assumes the Go runtime's signal
  handlers are installed. A program that installs its own `SIGSEGV`
  handler (through cgo) after loading a module bypasses it.
- **Profiling.** In the host's CPU profiles, time spent inside a module is
  attributed to `gig.enterModule`, not to the module's functions.
- **One module per directory.** A module is one package; it can't import
  packages from its own subdirectories.
