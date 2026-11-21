package gig

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"
)

const pageSize = 4096

// image is a module's ELF shared object mapped into memory and relocated.
type image struct {
	mem     []byte            // the whole mapping (all PT_LOAD segments)
	base    uintptr           // load address (vaddr 0 of the object)
	symbols map[string]uintptr // defined dynamic symbols
	funcs   symbolizer         // for fault reports
}

// loadImage maps the shared object in data, applies its dynamic relocations
// (resolving undefined symbols through host) and sets segment protections.
// It's a minimal dynamic loader: no libc, no dlopen, no cgo. go-in-go modules
// are linked with -Bsymbolic and have no dependencies, so they only use a
// handful of relocation types.
func loadImage(data []byte, host map[string]uintptr) (*image, error) {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if f.Class != elf.ELFCLASS64 || f.Machine != elf.EM_X86_64 || f.Type != elf.ET_DYN {
		return nil, fmt.Errorf("not an x86-64 ELF shared object (class %v, machine %v, type %v)", f.Class, f.Machine, f.Type)
	}

	// The address span of all loadable segments.
	var end uint64
	var loads []*elf.Prog
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD {
			continue
		}
		loads = append(loads, p)
		if e := p.Vaddr + p.Memsz; e > end {
			end = e
		}
	}
	if len(loads) == 0 {
		return nil, fmt.Errorf("no loadable segments")
	}
	size := int(roundUp(end, pageSize))
	mem, err := syscall.Mmap(-1, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		return nil, fmt.Errorf("mapping %d bytes: %w", size, err)
	}
	img := &image{mem: mem, base: uintptr(unsafe.Pointer(&mem[0])), symbols: map[string]uintptr{}, funcs: newSymbolizer(f)}
	ok := false
	defer func() {
		if !ok {
			syscall.Munmap(mem)
		}
	}()

	for _, p := range loads {
		if p.Off+p.Filesz > uint64(len(data)) {
			return nil, fmt.Errorf("segment at %#x is outside the file", p.Vaddr)
		}
		copy(mem[p.Vaddr:p.Vaddr+p.Filesz], data[p.Off:p.Off+p.Filesz])
	}

	syms, err := f.DynamicSymbols()
	if err != nil {
		return nil, fmt.Errorf("reading dynamic symbols: %w", err)
	}
	for _, s := range syms {
		if s.Section != elf.SHN_UNDEF && s.Name != "" {
			img.symbols[s.Name] = img.base + uintptr(s.Value)
		}
	}
	resolve := func(idx uint32) (uintptr, error) {
		if idx == 0 || int(idx) > len(syms) {
			return 0, fmt.Errorf("bad symbol index %d", idx)
		}
		s := syms[idx-1] // DynamicSymbols skips the null symbol
		if s.Section != elf.SHN_UNDEF {
			return img.base + uintptr(s.Value), nil
		}
		if a, ok := host[s.Name]; ok {
			return a, nil
		}
		return 0, fmt.Errorf("undefined symbol %q (the module needs something this loader doesn't provide)", s.Name)
	}

	for _, sec := range f.Sections {
		if sec.Type != elf.SHT_RELA {
			continue
		}
		rd, err := sec.Data()
		if err != nil {
			return nil, err
		}
		for i := 0; i+24 <= len(rd); i += 24 {
			off := binary.LittleEndian.Uint64(rd[i:])
			info := binary.LittleEndian.Uint64(rd[i+8:])
			addend := int64(binary.LittleEndian.Uint64(rd[i+16:]))
			typ := elf.R_X86_64(info & 0xffffffff)
			symIdx := uint32(info >> 32)
			if off+8 > uint64(len(mem)) {
				return nil, fmt.Errorf("relocation at %#x outside the image", off)
			}
			var v uint64
			switch typ {
			case elf.R_X86_64_RELATIVE:
				v = uint64(img.base) + uint64(addend)
			case elf.R_X86_64_64:
				s, err := resolve(symIdx)
				if err != nil {
					return nil, err
				}
				v = uint64(s) + uint64(addend)
			case elf.R_X86_64_GLOB_DAT, elf.R_X86_64_JMP_SLOT:
				s, err := resolve(symIdx)
				if err != nil {
					return nil, err
				}
				v = uint64(s)
			case elf.R_X86_64_NONE:
				continue
			default:
				return nil, fmt.Errorf("unsupported relocation %v at %#x", typ, off)
			}
			binary.LittleEndian.PutUint64(mem[off:], v)
		}
	}

	// Page protections: the union of the flags of the segments on each page.
	prot := make([]int, size/pageSize)
	for _, p := range loads {
		var pr int
		if p.Flags&elf.PF_R != 0 {
			pr |= syscall.PROT_READ
		}
		if p.Flags&elf.PF_W != 0 {
			pr |= syscall.PROT_WRITE
		}
		if p.Flags&elf.PF_X != 0 {
			pr |= syscall.PROT_EXEC
		}
		for pg := p.Vaddr / pageSize; pg < roundUp(p.Vaddr+p.Memsz, pageSize)/pageSize; pg++ {
			prot[pg] |= pr
		}
	}
	for start := 0; start < len(prot); {
		n := 1
		for start+n < len(prot) && prot[start+n] == prot[start] {
			n++
		}
		if err := syscall.Mprotect(mem[start*pageSize:(start+n)*pageSize], prot[start]); err != nil {
			return nil, fmt.Errorf("mprotect: %w", err)
		}
		start += n
	}
	ok = true
	return img, nil
}

func roundUp(v, to uint64) uint64 { return (v + to - 1) &^ (to - 1) }
