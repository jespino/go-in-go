// Package build turns a Go project into a go-in-go module.
//
// Pipeline:
//
//	project *.go ──scan //gig:export, //gig:import──▶ copy + zz_goingo.go (generated glue)
//	──tinygo build (goingo target, overlay TINYGOROOT)──▶ ELF shared object
//	──objcopy --add-section .goingo=manifest.json──▶ module file
package build

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jespino/go-in-go/gig"
	"github.com/jespino/go-in-go/internal/assets"
	"github.com/jespino/tinyguest/cli"
	"github.com/jespino/tinyguest/project"
	"github.com/jespino/tinyguest/toolchain"
)

// Options configure a build.
type Options struct {
	Dir     string // project directory (contains go.mod)
	Output  string // module file; default <Dir>/<name>.gig
	Opt     string // TinyGo optimization level; default "2"
	Verbose bool
	Log     io.Writer
}

// Build builds the module and returns the output path.
func Build(opts Options) (string, error) {
	if opts.Log == nil {
		opts.Log = os.Stderr
	}
	if opts.Opt == "" {
		opts.Opt = "2"
	}
	proj, err := filepath.Abs(opts.Dir)
	if err != nil {
		return "", err
	}
	mod, err := project.ReadGoMod(filepath.Join(proj, "go.mod"), "")
	if err != nil {
		return "", err
	}
	modPath := mod.Path
	out := opts.Output
	if out == "" {
		out = filepath.Join(proj, filepath.Base(modPath)+".gig")
	}
	// objcopy runs in the work directory: a relative -o means relative to
	// the caller's directory.
	if out, err = filepath.Abs(out); err != nil {
		return "", err
	}
	for _, tool := range []string{"tinygo", "objcopy"} {
		if _, err := exec.LookPath(tool); err != nil {
			return "", fmt.Errorf("%s not found in PATH", tool)
		}
	}
	cache, err := toolchain.CacheDir("go-in-go", "GOINGOCACHE")
	if err != nil {
		return "", err
	}
	tg, err := toolchain.Find()
	if err != nil {
		return "", err
	}
	root, err := assets.Prepare(cache, tg)
	if err != nil {
		return "", err
	}
	work := filepath.Join(cache, "build", project.ShortHash(proj))
	src := filepath.Join(work, "src")

	res, err := prepareSource(proj, src)
	if err != nil {
		return "", err
	}
	gen, err := genModule(res)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "zz_goingo.go"), gen, 0o644); err != nil {
		return "", err
	}
	r := &cli.Runner{Tool: "go-in-go", Log: opts.Log, Verbose: opts.Verbose}
	r.Step("%s: %d exports, %d imports", modPath, len(res.exports), len(res.imports))

	so := filepath.Join(work, "module.so")
	args := []string{"build", "-target=" + root.Target, "-opt=" + opts.Opt, "-no-debug", "-o", so, "."}
	cmd := exec.Command("tinygo", args...)
	cmd.Dir = src
	cmd.Env = toolchain.Env(root.TinyGoRoot, "off")
	if err := r.Exec(cmd); err != nil {
		return "", err
	}

	m := gig.Manifest{
		ABI:     gig.ABIVersion,
		Module:  modPath,
		TinyGo:  tg.ShortVersion(),
		Exports: res.exports,
		Imports: res.imports,
	}
	if m.Exports == nil {
		m.Exports = []gig.Func{}
	}
	if m.Imports == nil {
		m.Imports = []gig.Func{}
	}
	mj, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	mf := filepath.Join(work, "manifest.json")
	if err := os.WriteFile(mf, mj, 0o644); err != nil {
		return "", err
	}
	cmd = exec.Command("objcopy", "--add-section", gig.ManifestSection+"="+mf,
		"--set-section-flags", gig.ManifestSection+"=readonly", so, out)
	cmd.Dir = work
	if err := r.Exec(cmd); err != nil {
		return "", err
	}
	return out, nil
}
