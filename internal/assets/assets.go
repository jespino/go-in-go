// Package assets holds the TinyGo target and runtime layer for go-in-go
// modules, and materializes them in the user's cache directory (with
// tinyguest/assets).
//
// _files/ layout:
//
//	target.json   TinyGo target: freestanding x86-64 ELF shared object
//	overlay/      files added on top of TinyGo's source tree (runtime platform)
//
// The directory name starts with "_" so the go tool doesn't treat the Go files
// in it (which only build with TinyGo for the goingo target) as packages.
package assets

import (
	"embed"
	"io/fs"

	tgassets "github.com/jespino/tinyguest/assets"
	"github.com/jespino/tinyguest/toolchain"
)

//go:embed _files
var files embed.FS

// Platform is the build tag of the runtime layer in overlay/.
const Platform = "goingo"

// excludedFiles are TinyGo source files (relative to the root) that would
// otherwise build for the goingo target but conflict with its runtime layer:
// baremetal_memory.go has a fixed-size heap (memory_goingo.go grows it).
var excludedFiles = []string{
	"src/runtime/baremetal_memory.go",
}

// Root is a materialized copy of the assets plus a TinyGo root overlay.
type Root = tgassets.Root

// Prepare materializes the assets for the given TinyGo under cacheDir,
// reusing a previous copy when nothing changed.
func Prepare(cacheDir string, tg *toolchain.TinyGo) (*Root, error) {
	sub, err := fs.Sub(files, "_files")
	if err != nil {
		return nil, err
	}
	return tgassets.Prepare(tgassets.Config{
		Files:    sub,
		Platform: Platform,
		Exclude:  excludedFiles,
		CacheDir: cacheDir,
		TinyGo:   tg,
	})
}
