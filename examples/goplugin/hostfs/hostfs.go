// Package hostfs is the file access the go-plugin examples' hosts give their
// plugins.
//
// In go-plugin, a plugin is a separate process and opens files itself. A
// go-in-go module has no operating system: it reads and writes files through
// imports, implemented here. That puts the host in charge: Dir only lets a
// plugin use plain file names inside one directory.
package hostfs

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir gives plugins access to the files directly inside a directory.
type Dir string

// Path returns the path of the file name, or an error if name isn't a plain
// file name.
func (d Dir) Path(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || !filepath.IsLocal(name) {
		return "", fmt.Errorf("hostfs: %q: plugins may only use plain file names", name)
	}
	return filepath.Join(string(d), name), nil
}

// ReadFile returns the contents of the file name.
func (d Dir) ReadFile(name string) ([]byte, error) {
	p, err := d.Path(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// WriteFile replaces the contents of the file name.
func (d Dir) WriteFile(name string, data []byte) error {
	p, err := d.Path(name)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
