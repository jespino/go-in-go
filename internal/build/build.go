// Package build turns a Go project into a go-in-go module.
//
// Pipeline:
//
//	project *.go ──scan //gig:export, //gig:import──▶ copy + zz_goingo.go (generated glue)
//	──tinygo build (goingo target, overlay TINYGOROOT)──▶ ELF shared object
//	──objcopy --add-section .goingo=manifest.json──▶ module file
package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jespino/go-in-go/gig"
	"github.com/jespino/go-in-go/internal/assets"
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
	modPath, err := modulePath(filepath.Join(proj, "go.mod"))
	if err != nil {
		return "", err
	}
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
	cache, err := cacheDir()
	if err != nil {
		return "", err
	}
	tgRoot, tgVersion, err := tinygoInfo()
	if err != nil {
		return "", err
	}
	root, err := assets.Prepare(cache, tgRoot, tgVersion)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(proj))
	work := filepath.Join(cache, "build", hex.EncodeToString(sum[:8]))
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
	fmt.Fprintf(opts.Log, "go-in-go: %s: %d exports, %d imports\n", modPath, len(res.exports), len(res.imports))

	so := filepath.Join(work, "module.so")
	args := []string{"build", "-target=" + root.Target, "-opt=" + opts.Opt, "-no-debug", "-o", so, "."}
	if err := run(opts, src, tinygoEnv(root.TinyGoRoot), "tinygo", args...); err != nil {
		return "", err
	}

	m := gig.Manifest{
		ABI:     gig.ABIVersion,
		Module:  modPath,
		TinyGo:  shortVersion(tgVersion),
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
	if err := run(opts, work, nil, "objcopy", "--add-section", gig.ManifestSection+"="+mf,
		"--set-section-flags", gig.ManifestSection+"=readonly", so, out); err != nil {
		return "", err
	}
	return out, nil
}

// tinygoEnv is the environment for TinyGo. GOTOOLCHAIN is dropped: TinyGo
// needs the Go toolchain it supports, whatever Go version is used for the
// host (e.g. when building a module from a test run with an older Go).
func tinygoEnv(root string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOTOOLCHAIN=") {
			env = append(env, kv)
		}
	}
	return append(env, "TINYGOROOT="+root, "GOWORK=off")
}

func run(opts Options, dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	if opts.Verbose {
		fmt.Fprintf(opts.Log, "+ %s %s\n", name, strings.Join(args, " "))
	}
	outb, err := cmd.CombinedOutput()
	if len(outb) > 0 && (opts.Verbose || err != nil) {
		opts.Log.Write(outb)
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

var moduleRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)

func modulePath(goMod string) (string, error) {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return "", err
	}
	m := moduleRe.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("%s: no module line", goMod)
	}
	return strings.Trim(string(m[1]), `"`), nil
}

func cacheDir() (string, error) {
	if d := os.Getenv("GOINGOCACHE"); d != "" {
		return d, nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "go-in-go"), nil
}

func tinygoInfo() (root, version string, err error) {
	if _, err := exec.LookPath("tinygo"); err != nil {
		return "", "", errors.New("tinygo not found in PATH (https://tinygo.org/getting-started/install/)")
	}
	root = os.Getenv("TINYGOROOT")
	if root == "" {
		out, err := exec.Command("tinygo", "env", "TINYGOROOT").Output()
		if err != nil {
			return "", "", fmt.Errorf("tinygo env TINYGOROOT: %w", err)
		}
		root = strings.TrimSpace(string(out))
	}
	out, err := exec.Command("tinygo", "version").Output()
	if err != nil {
		return "", "", fmt.Errorf("tinygo version: %w", err)
	}
	return root, strings.TrimSpace(string(out)), nil
}

// shortVersion turns "tinygo version 0.44.0 linux/amd64 (using ...)" into "0.44.0".
func shortVersion(v string) string {
	f := strings.Fields(v)
	if len(f) >= 3 {
		return f[2]
	}
	return v
}
