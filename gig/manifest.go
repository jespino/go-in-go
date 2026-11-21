package gig

import (
	"bytes"
	"debug/elf"
	"encoding/json"
	"fmt"
	"strings"
)

// ABIVersion is the version of the module <-> host interface: the call
// trampolines, the host call numbers, the symbols the loader provides and the
// way values are passed. A loader only accepts modules built for its ABI
// version. Note what it does *not* depend on: the Go version of the host.
const ABIVersion = 2

// ManifestSection is the ELF section holding a module's JSON manifest.
const ManifestSection = ".goingo"

// Manifest describes a module: the functions it exports to the host and the
// host functions it imports.
type Manifest struct {
	ABI     int    `json:"abi"`
	Module  string `json:"module"`  // Go module path of the source
	TinyGo  string `json:"tinygo"`  // TinyGo version that built it
	Exports []Func `json:"exports"` // callable by the host
	Imports []Func `json:"imports"` // provided by the host
}

// Func is the signature of an exported or imported function.
type Func struct {
	Name    string  `json:"name"`
	Doc     string  `json:"doc,omitempty"`
	Params  []Param `json:"params"`
	Results []Param `json:"results"`
}

// Param is a parameter or result. Type is one of the Types.
type Param struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type"`
}

// Types are the Go types that can cross the module boundary. Everything is
// passed as 64-bit slots: string and []byte take two (pointer, length),
// error takes two (pointer, length+1, with 0 meaning nil), everything else
// one. Values are always copied: neither side keeps pointers into the other
// side's memory after the call.
var Types = []string{
	"bool", "int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"float32", "float64", "string", "[]byte", "error",
}

// MaxArgSlots and MaxRetSlots bound the flattened size of a signature.
const (
	MaxArgSlots = 64
	MaxRetSlots = 32
)

// Slots returns the number of 64-bit slots a value of type t takes.
func Slots(t string) int {
	switch t {
	case "string", "[]byte", "error":
		return 2
	}
	return 1
}

// ValidType reports whether t can cross the module boundary.
func ValidType(t string) bool {
	for _, v := range Types {
		if v == t {
			return true
		}
	}
	return false
}

// ArgSlots is the number of slots of the parameters.
func (f *Func) ArgSlots() int { return countSlots(f.Params) }

// RetSlots is the number of slots of the results.
func (f *Func) RetSlots() int { return countSlots(f.Results) }

func countSlots(ps []Param) int {
	n := 0
	for _, p := range ps {
		n += Slots(p.Type)
	}
	return n
}

// Signature returns the signature as "(string,int)(string,error)".
func (f *Func) Signature() string {
	var sb strings.Builder
	sb.WriteByte('(')
	for i, p := range f.Params {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(p.Type)
	}
	sb.WriteString(")(")
	for i, p := range f.Results {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(p.Type)
	}
	sb.WriteByte(')')
	return sb.String()
}

// ReadManifest extracts the manifest of a module file's contents.
func ReadManifest(data []byte) (*Manifest, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not an ELF file: %w", err)
	}
	sec := f.Section(ManifestSection)
	if sec == nil {
		return nil, fmt.Errorf("no %s section: not a go-in-go module", ManifestSection)
	}
	raw, err := sec.Data()
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("bad manifest: %w", err)
	}
	if m.ABI != ABIVersion {
		return nil, fmt.Errorf("module uses go-in-go ABI %d, this loader supports ABI %d", m.ABI, ABIVersion)
	}
	return &m, nil
}
