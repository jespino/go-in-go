// Command go-in-go builds Go code into modules that any Go program can load,
// whatever Go version it's built with.
//
//	go-in-go build [-o out.gig] [dir]            build a module with TinyGo
//	go-in-go bindgen -pkg name [-o file.go] mod  generate typed host bindings
//	go-in-go inspect mod                         print a module's manifest
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jespino/go-in-go/gig"
	"github.com/jespino/go-in-go/internal/build"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  go-in-go build [-o out.gig] [-v] [dir]          build the module in dir (default .)
  go-in-go bindgen -pkg name [-o file.go] mod.gig  generate typed host bindings
  go-in-go inspect mod.gig                         print the module's manifest
`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "build":
		err = cmdBuild(args)
	case "bindgen":
		err = cmdBindgen(args)
	case "inspect":
		err = cmdInspect(args)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "go-in-go:", err)
		os.Exit(1)
	}
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("o", "", "output module file (default <dir>/<module name>.gig)")
	opt := fs.String("opt", "2", "TinyGo optimization level")
	verbose := fs.Bool("v", false, "print commands")
	fs.Parse(args)
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	path, err := build.Build(build.Options{Dir: dir, Output: *out, Opt: *opt, Verbose: *verbose})
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "go-in-go: wrote", path)
	return nil
}

func readManifest(path string) (*gig.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return gig.ReadManifest(data)
}

func cmdBindgen(args []string) error {
	fs := flag.NewFlagSet("bindgen", flag.ExitOnError)
	pkg := fs.String("pkg", "", "package name of the bindings (required)")
	out := fs.String("o", "", "output file (default stdout)")
	fs.Parse(args)
	if *pkg == "" || fs.NArg() != 1 {
		usage()
	}
	m, err := readManifest(fs.Arg(0))
	if err != nil {
		return err
	}
	src, err := build.Bindgen(m, *pkg, filepath.Base(fs.Arg(0)))
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = os.Stdout.Write(src)
		return err
	}
	return os.WriteFile(*out, src, 0o644)
}

func cmdInspect(args []string) error {
	if len(args) != 1 {
		usage()
	}
	m, err := readManifest(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("module %s (go-in-go ABI %d, built with TinyGo %s)\n", m.Module, m.ABI, m.TinyGo)
	for _, f := range m.Exports {
		fmt.Printf("  export %s%s\n", f.Name, f.Signature())
	}
	for _, f := range m.Imports {
		fmt.Printf("  import %s%s\n", f.Name, f.Signature())
	}
	if os.Getenv("GOINGO_JSON") != "" {
		j, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(j))
	}
	return nil
}
