---
title: "Tutorial"
description: "Build a \"hello world\" module, call it from a Go program built with an older Go version, then add a function of your own."
weight: 2
---

This tutorial builds a tiny module with go-in-go, loads it from a Go
program, then shows that the same module works with a host built with a
different Go version. It takes about ten minutes. The rest of the
[documentation](/docs/) is the full reference.

## 1. What you need

- Linux x86-64
- Go and [TinyGo](https://tinygo.org/getting-started/install/) (tested with
  TinyGo 0.44.0)
- binutils (`objcopy`)
- The `go-in-go` command: see [Installation](/docs/install/)

## 2. Create the module

A module is a Go `main` package in a directory with its own `go.mod`:

```sh
mkdir -p tutorial/hello tutorial/app
cd tutorial/hello
go mod init hello
```

The module path (`hello`) is the module's name. Write `main.go`:

```go
package main

import (
	"errors"
	"fmt"
	"strings"
)

// Log writes a message through the host's logger.
//
//gig:import
func Log(msg string)

var greeted int

// Greet returns a greeting for name.
//
//gig:export
func Greet(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty name")
	}
	greeted++
	Log(fmt.Sprintf("greeting #%d", greeted))
	return "Hello, " + name + "!", nil
}

// Shout upper-cases s.
//
//gig:export
func Shout(s string) string {
	return strings.ToUpper(s) + "!"
}

func main() {
	Log("hello module loaded")
}
```

Here's how go-in-go reads it:

- **`//gig:export`** makes the function callable by the host. Its
  parameters and results must be types that can cross the boundary:
  numbers, `bool`, `string`, `[]byte`, and `error` as the last result.
- **`//gig:import`** declares a function, without a body, that the host
  provides. The module calls it like any other function.
- **Global state** (`greeted`) lives in the module and survives between
  calls, for as long as the module stays loaded.
- **`main`** runs once, when the module is loaded, after the package
  initializers. Here it calls the host.

## 3. Build it

```sh
go-in-go build
```

This compiles the package with TinyGo into `hello.gig`: a native x86-64
file with its own small Go runtime, plus a manifest of its exports and
imports. See what it contains:

```sh
go-in-go inspect hello.gig
# module hello (go-in-go ABI 2, built with TinyGo 0.44.0)
#   export Greet(string)(string,error)
#   export Shout(string)(string)
#   import Log(string)()
```

## 4. Generate the host bindings

The host program calls the module through a small generated package:

```sh
cd ../app
go mod init app
go mod edit -go=1.22    # so that older Go versions can build it too (step 6)
go get github.com/jespino/go-in-go/gig
mkdir hello
go-in-go bindgen -pkg hello -o hello/hello_gig.go ../hello/hello.gig
```

`hello/hello_gig.go` contains:

- a `Module` type, with a method per export: `Greet(name string) (string, error)`
  and `Shout(s string) (string, error)`. Every method returns an `error`
  for problems with the call itself; `Greet` already had one, so it's
  shared.
- an `Imports` interface, which your program implements: `Log(msg string)`.
- `Load` and `LoadBytes`, which load the module and check that its
  signatures match the bindings.

## 5. Call it

Write `app/main.go`:

```go
package main

import (
	"fmt"
	"log"
	"runtime"

	"app/hello"
)

// host implements hello.Imports.
type host struct{}

func (host) Log(msg string) { log.Println("module:", msg) }

func main() {
	log.SetFlags(0)
	fmt.Println("host built with", runtime.Version())

	m, err := hello.Load("../hello/hello.gig", host{}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer m.Close()

	s, err := m.Greet("gopher")
	fmt.Println(s, err)

	_, err = m.Greet("")
	fmt.Println("error from the module:", err)

	s, _ = m.Shout("go inside go")
	fmt.Println(s)
}
```

Run it:

```sh
go run .
# host built with go1.26.8
# module: hello module loaded
# module: greeting #1
# Hello, gopher! <nil>
# error from the module: empty name
# GO INSIDE GO!
```

## 6. Change the host's Go version

This is the point of go-in-go. Without rebuilding the module, build the
host with another Go version, and without cgo:

```sh
GOTOOLCHAIN=go1.22.12 CGO_ENABLED=0 go run .
# host built with go1.22.12
# module: hello module loaded
# ...
```

The `hello.gig` file didn't change. It doesn't know or care which Go
version the host uses.

## 7. Add your own function

Add this to `hello/main.go`:

```go
// Words counts the words in text.
//
//gig:export
func Words(text string) int {
	return len(strings.Fields(text))
}
```

Rebuild the module and regenerate the bindings:

```sh
cd ../hello && go-in-go build
cd ../app && go-in-go bindgen -pkg hello -o hello/hello_gig.go ../hello/hello.gig
```

Now `m.Words("go in go")` returns `3, nil`. If you rebuild the module but
forget to regenerate the bindings, `Load` fails with an error naming the
function whose signature changed, instead of calling it with the wrong
arguments.

## 8. Where to go next

- [Writing modules](/docs/modules/): all the types, errors, goroutines and
  what the standard library offers.
- [Loading modules](/docs/host/): options, error handling, pools and
  background goroutines.
- [How it works](/docs/internals/): what happens when you call `Greet`.
