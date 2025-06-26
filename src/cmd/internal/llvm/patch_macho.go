// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.tangollvm && linux && amd64 && cgo && !compiler_bootstrap

package llvm

import (
	"bytes"
	"cmd/internal/objabi"
	"debug/macho"
	"fmt"
	"sort"
	"strings"
)

const typeTypeMask = 0xee
const stabTypeMask = 0xe0

const N_SECT = 0xe

type uint64s []uint64

func (x uint64s) Len() int           { return len(x) }
func (x uint64s) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }
func (x uint64s) Less(i, j int) bool { return x[i] < x[j] }

type GoReloc []TargetReloc

func (x GoReloc) Len() int           { return len(x) }
func (x GoReloc) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }
func (x GoReloc) Less(i, j int) bool { return x[i].Off < x[j].Off }

func lookupMachOSymbol(pctxt PatchContext, name string) TargetSym {
	// See src/debug/macho/file.go
	// file.go has strip leading _ for those has dot
	// This function is introduced for symbol such as type:string
	if !strings.Contains(name, ".") && strings.Contains(name, ":") && name[0] == '_' {
		name = name[1:]
	}
	return pctxt.LookupSym(name)
}

// https://opensource.apple.com/source/xnu/xnu-7195.81.3/EXTERNAL_HEADERS/mach-o/
func extractSymbolsFromMachO(pctxt PatchContext, b []byte) map[string]*TargetSymbol {
	r := bytes.NewReader(b)
	f, err := macho.NewFile(r)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if f.Symtab == nil {
		panic(fmt.Errorf("no symbol table"))
	}
	var auxmap = make(map[string]*TargetSymbol)
	var addrs []uint64
	for _, s := range f.Symtab.Syms {
		// Skip stab debug info.
		if s.Type&stabTypeMask != 0 {
			continue
		}
		// skip symbol not defined in any section
		if s.Type&typeTypeMask != N_SECT {
			continue
		}
		if s.Sect == 0 || int(s.Sect) > len(f.Sections) {
			continue
		}
		addrs = append(addrs, s.Value)
	}
	sort.Sort(uint64s(addrs))
	var sectData = make([][]byte, len(f.Sections))
	var loadData = func(f *macho.File, seci uint8) []byte {
		if sectData[seci] == nil {
			if d, err := f.Sections[seci].Data(); err == nil {
				sectData[seci] = d
				return sectData[seci]
			} else {
				panic(err)
			}
		}
		return sectData[seci]
	}
	getSectionSymbol := func(f *macho.File, sectid uint8) TargetSym {
		if sectid == 0 || int(sectid) > len(f.Sections) {
			return nil
		}
		sect := f.Sections[sectid-1]
		if sect.Seg != "__TEXT" {
			return nil
		}
		data := loadData(f, sectid-1)
		return pctxt.CreateConstDataSymbol(data, int64(len(data)), 1<<sect.Align)
	}
	getConstantSymbol := func(f *macho.File, symid uint32) (TargetSym, uint64) {
		s := &f.Symtab.Syms[symid]
		if s.Sect == 0 || int(s.Sect) > len(f.Sections) {
			return nil, 0
		}
		sect := f.Sections[s.Sect-1]
		if sect.Seg != "__TEXT" {
			return nil, 0
		}
		off := s.Value - sect.Addr
		sym := getSectionSymbol(f, s.Sect)
		return sym, off
	}
	for i := range f.Symtab.Syms {
		s := &f.Symtab.Syms[i]
		if s.Type&stabTypeMask != 0 {
			// Skip stab debug info.
			continue
		}
		// skip symbol not defined in any section
		if s.Type&typeTypeMask != N_SECT {
			continue
		}
		if s.Sect == 0 || int(s.Sect) > len(f.Sections) {
			continue
		}
		sect := f.Sections[s.Sect-1]
		// TODO: use Seg and Name to filter out special sections.
		if sect.Seg != "__TEXT" || !(sect.Name == "__text" || sect.Name == "__const") {
			continue
		}
		sym := TargetSymbol{HostName: s.Name}
		idx := sort.Search(len(addrs), func(x int) bool { return addrs[x] > s.Value })
		sectend := sect.Size + sect.Addr
		if idx < len(addrs) {
			if addrs[idx] < sectend {
				sym.Size = addrs[idx] - s.Value
			} else {
				// the last entry of this section?
				sym.Size = sectend - s.Value
			}
		} else {
			// the last entry of this section and this section is the last section?
			sym.Size = sectend - s.Value
		}
		data := loadData(f, s.Sect-1)
		symoff := s.Value - sect.Addr
		sym.P = data[symoff : symoff+sym.Size]
		end := len(sect.Relocs)
		idx = sort.Search(end, func(i int) bool {
			mr := &sect.Relocs[i]
			absAddr := uint64(mr.Addr) + sect.Addr
			if absAddr >= s.Value+sym.Size {
				return false
			}
			return true
		})
		for ; idx < end; idx++ {
			mr := &sect.Relocs[idx]
			absAddr := uint64(mr.Addr) + sect.Addr
			if !(s.Value <= uint64(absAddr) && uint64(absAddr) < s.Value+sym.Size) {
				break
			}
			r := TargetReloc{}
			// offset within symbol data
			r.Off = int32(absAddr - s.Value)
			if mr.Scattered == true {
				panic("No scattered in amd64")
			}
			mrtype := macho.RelocTypeX86_64(mr.Type)
			switch mrtype {
			case macho.X86_64_RELOC_UNSIGNED:
				r.Type = objabi.R_ADDR
				if mr.Pcrel == true {
					panic("must be absolute")
				}
				/* 0=byte, 1=word, 2=long, 3=quad */
				if mr.Len != 2 {
					panic("must be long")
				}
				r.Siz = 4
				if int32(f.ByteOrder.Uint32(sym.P[r.Off:])) != 0 {
					panic("TODO:")
				}
			case macho.X86_64_RELOC_SIGNED, macho.X86_64_RELOC_SIGNED_1, macho.X86_64_RELOC_SIGNED_2, macho.X86_64_RELOC_SIGNED_4:
				// https://github.com/apple-oss-distributions/xnu/blob/main/EXTERNAL_HEADERS/mach-o/x86_64/reloc.h
				/* 0=byte, 1=word, 2=long, 3=quad */
				if mr.Len != 2 {
					panic("must be long")
				}
				if mr.Pcrel == false {
					panic("must be pcrel")
				}
				r.Siz = 4
				// read in-place addend for PCREL relocation
				addend := int64(int32(f.ByteOrder.Uint32(data[mr.Addr : mr.Addr+4])))
				// clear in-place addend after we read it
				f.ByteOrder.PutUint32(data[mr.Addr:mr.Addr+4], 0)

				extraAddend := int64(0)
				switch mrtype {
				case macho.X86_64_RELOC_SIGNED_1:
					extraAddend = 1
				case macho.X86_64_RELOC_SIGNED_2:
					extraAddend = 2
				case macho.X86_64_RELOC_SIGNED_4:
					extraAddend = 4
				}
				// r.Add = addend + extraAddend

				if mr.Extern {
					if mr.Scattered != false {
						panic("scattered must be false")
					}
					r.Type = objabi.R_PCREL
					if !(0 <= int(mr.Value) && int(mr.Value) < len(f.Symtab.Syms)) {
						panic("cannot find symbol for this reloc")
					}
					// TODO: we make global internal linkage.
					// force extern if we make global external linkage
					name := f.Symtab.Syms[mr.Value].Name
					r.HostName = name
					rsym := pctxt.FindSym(name)
					if rsym != nil {
						r.Sym = rsym
					} else {
						rsym, roff := getConstantSymbol(f, mr.Value)
						if rsym != nil {
							r.Sym = rsym
							// fmt.Println(mrtype, "name", rsym.Name, name, roff, addend, extraAddend, r.Add, "in", s.Name)
							r.Add = (int64(roff) + addend + extraAddend) - extraAddend
							// fmt.Println(mrtype, "name", rsym.Name, name, roff, addend, extraAddend, r.Add)
						}
					}
					if r.Sym == nil {
						// TODO: raise error by calling lookup
						r.Sym = lookupMachOSymbol(pctxt, name)
					}
				} else {
					if mr.Scattered != false {
						panic("scattered must be false")
					}
					r.Type = objabi.R_PCREL
					rsect := f.Sections[mr.Value-1]

					// the offset of the reloc value relative to the reloc data section
					// mr.Addr + 4 + addend is the address of the next instruction
					off := (mr.Addr + uint32(sect.Addr) + uint32(4+extraAddend+addend)) - uint32(rsect.Addr)

					if extraAddend != 0 {
						panic("non zero extraAddend")
					}
					// https://github.com/aidansteele/osx-abi-macho-file-format-reference
					if rsect.Seg == "__TEXT" && (rsect.Name == "__literal4" || rsect.Name == "__literal8" || rsect.Name == "__literal16" || rsect.Name == "__const") {
						r.Sym = getSectionSymbol(f, uint8(mr.Value))
						r.Add = int64(off)
					} else {
						panic(fmt.Errorf("TODO: unknown section: %v", rsect))
					}
				}
			case macho.X86_64_RELOC_BRANCH:
				r.Type = objabi.R_CALL
				/* 0=byte, 1=word, 2=long, 3=quad */
				if mr.Len != 2 {
					panic("must be long")
				}
				if mr.Pcrel == false {
					panic("must be pcrel")
				}
				r.Siz = 4
				if mr.Extern == false {
					panic("TODO: must be extern")
				}
				name := f.Symtab.Syms[mr.Value].Name
				r.HostName = name
				r.Sym = lookupMachOSymbol(pctxt, name)
				addend := int32(f.ByteOrder.Uint32(sym.P[r.Off:]))
				if addend != 0 {
					if name == "runtime.duffcopy" || name == "runtime.duffzero" {
						r.Add = int64(addend)
					} else {
						panic("TODO:")
					}
				}
			case macho.X86_64_RELOC_GOT_LOAD:
				r.Type = objabi.R_PCREL
				/* 0=byte, 1=word, 2=long, 3=quad */
				if mr.Len != 2 {
					panic("must be long")
				}
				if mr.Pcrel == false {
					panic("must be pcrel")
				}
				r.Siz = 4
				if mr.Extern == false {
					panic("TODO: must be extern")
				}
				name := f.Symtab.Syms[mr.Value].Name
				r.Sym = lookupMachOSymbol(pctxt, name) // sym should not be null
				addend := int32(f.ByteOrder.Uint32(sym.P[r.Off:]))
				if addend != 0 {
					panic("TODO:")
				}
			case macho.X86_64_RELOC_GOT:
				/* 0=byte, 1=word, 2=long, 3=quad */
				if mr.Len != 2 {
					panic("must be long")
				}
				r.Siz = 4
				if mr.Pcrel == true {
					r.Type = objabi.R_PCREL
				} else {
					// RELOC GOT ADDR for InlTree
					r.Type = objabi.R_ADDR
				}
				if mr.Extern == false {
					panic("TODO: must be extern")
				}
				name := f.Symtab.Syms[mr.Value].Name
				r.HostName = name
				r.Sym = lookupMachOSymbol(pctxt, name) // sym should not be null
				addend := int32(f.ByteOrder.Uint32(sym.P[r.Off:]))
				f.ByteOrder.PutUint32(sym.P[r.Off:], 0)
				r.Add = int64(addend)
			default:
				panic(fmt.Errorf("TODO: unsupported MachO reloc: %s", macho.RelocTypeX86_64(mr.Type)))
			}
			sym.R = append(sym.R, r)
		}
		sort.Sort(GoReloc(sym.R))
		auxmap[sym.HostName] = &sym
	}
	return auxmap
}
