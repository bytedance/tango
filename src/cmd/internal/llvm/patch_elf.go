// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.tangollvm && linux && amd64 && cgo && !compiler_bootstrap

package llvm

import (
	"bytes"
	"cmd/internal/objabi"
	"debug/elf"
	"fmt"
	"sort"
)

type rawElfFile struct {
	f            *elf.File
	symtab       []elf.Symbol
	secsym       [][]*elf.Symbol
	secdat       [][]byte
	secgosym     []TargetSym
	symDataCache map[elf.Symbol][]byte
	relocCache   map[elf.SectionIndex][]relocData
	pctxt        PatchContext
}
type elfRelocInfo struct {
	off    uint64
	info   uint64
	addend int64
}

type relocData struct {
	section *elf.Section
	relocs  []elfRelocInfo
}

func (rf *rawElfFile) init() {
	rf.secsym = make([][]*elf.Symbol, len(rf.f.Sections))
	rf.secdat = make([][]byte, len(rf.f.Sections))
	rf.secgosym = make([]TargetSym, len(rf.f.Sections))
	rf.symDataCache = make(map[elf.Symbol][]byte)
	rf.relocCache = make(map[elf.SectionIndex][]relocData)

	for i := range rf.symtab {
		elfSym := &rf.symtab[i]
		secti := elfSym.Section
		if secti < 0 || int(secti) >= len(rf.f.Sections) {
			continue
		}
		if rf.secsym[secti] == nil {
			rf.secsym[secti] = make([]*elf.Symbol, 0)
		}
		rf.secsym[secti] = append(rf.secsym[secti], elfSym)
	}
	for i := range rf.secsym {
		if rf.secsym[i] == nil {
			continue
		}
		sort.Slice(rf.secsym[i], func(a, b int) bool {
			return rf.secsym[i][a].Value < rf.secsym[i][b].Value
		})
	}

	for i, section := range rf.f.Sections {
		if section.Type != elf.SHT_RELA && section.Type != elf.SHT_REL {
			continue
		}
		targetIdx := elf.SectionIndex(section.Info)
		data := rf.sectionData(elf.SectionIndex(i))
		rd := relocData{section: section}

		if section.Type == elf.SHT_RELA {
			rd.relocs = rf.precomputeRela(data)
		} else {
			rd.relocs = rf.precomputeRel(data)
		}

		rf.relocCache[targetIdx] = append(rf.relocCache[targetIdx], rd)
	}
}

func (rf *rawElfFile) precomputeRela(rd []byte) []elfRelocInfo {
	if rf.f.Class != elf.ELFCLASS64 || rf.f.Machine != elf.EM_X86_64 {
		panic("only support amd64")
	}
	if len(rd)%24 != 0 {
		panic("must be 24 aligned")
	}

	relocs := make([]elfRelocInfo, 0, len(rd)/24)
	byteOrder := rf.f.ByteOrder

	for i := 0; i < len(rd); i += 24 {
		relocs = append(relocs, elfRelocInfo{
			off:    byteOrder.Uint64(rd[i : i+8]),
			info:   byteOrder.Uint64(rd[i+8 : i+16]),
			addend: int64(byteOrder.Uint64(rd[i+16 : i+24])),
		})
	}

	return relocs
}

func (rf *rawElfFile) precomputeRel(rd []byte) []elfRelocInfo {
	if rf.f.Class != elf.ELFCLASS64 || rf.f.Machine != elf.EM_X86_64 {
		panic("only support amd64")
	}
	if len(rd)%16 != 0 {
		panic("must be 16 aligned")
	}

	relocs := make([]elfRelocInfo, 0, len(rd)/16)
	byteOrder := rf.f.ByteOrder

	for i := 0; i < len(rd); i += 16 {
		relocs = append(relocs, elfRelocInfo{
			off:    byteOrder.Uint64(rd[i : i+8]),
			info:   byteOrder.Uint64(rd[i+8 : i+16]),
			addend: 0,
		})
	}

	return relocs
}

func (rf *rawElfFile) processRelocations(relocDatas []elfRelocInfo, elfSym elf.Symbol, goSym *TargetSymbol) []TargetReloc {
	var result []TargetReloc

	start := sort.Search(len(relocDatas), func(i int) bool {
		return relocDatas[i].off >= elfSym.Value
	})

	if start == len(relocDatas) || relocDatas[start].off >= elfSym.Value+goSym.Size {
		return result
	}

	for i := start; i < len(relocDatas) && relocDatas[i].off < elfSym.Value+goSym.Size; i++ {
		reloc := rf.convertElfReloc64(relocDatas[i].off, relocDatas[i].info, relocDatas[i].addend, elfSym, goSym)
		result = append(result, reloc)
	}

	return result
}

func (rf *rawElfFile) sectionDataSym(i elf.SectionIndex) TargetSym {
	s := rf.secgosym[i]
	if s != nil {
		return s
	}
	d := rf.sectionData(i)
	s = rf.pctxt.CreateConstDataSymbol(d, int64(len(d)), int32(rf.f.Sections[i].Addralign))
	rf.secgosym[i] = s
	return s
}

func (rf *rawElfFile) sectionData(i elf.SectionIndex) []byte {
	if rf.secdat[i] == nil {
		if d, err := rf.f.Sections[i].Data(); err == nil {
			rf.secdat[i] = d
			return rf.secdat[i]
		} else {
			panic(err)
		}
	}
	return rf.secdat[i]
}

func (rf *rawElfFile) symData(e elf.Symbol) []byte {
	off := e.Value
	if e.Size > 0 {
		data := rf.sectionData(e.Section)
		return data[off : off+e.Size]
	}
	secs := rf.secsym[e.Section]
	data := rf.sectionData(e.Section)
	i := sort.Search(len(secs), func(i int) bool {
		return secs[i].Value > e.Value
	})
	if i < len(secs) {
		if off >= secs[i].Value {
			panic("sanity check failed!")
		}
		return data[off:secs[i].Value]
	} else {
		return data[off:]
	}
}

func (rf *rawElfFile) extractSymbolsFromELF() map[string]*TargetSymbol {
	var symmap = make(map[string]*TargetSymbol)
	elfSyms := rf.symtab
	for _, elfSym := range elfSyms {
		if elfSym.Section == elf.SHN_UNDEF {
			continue
		}
		if int(elfSym.Section) < 0 || int(elfSym.Section) > len(rf.f.Sections) {
			continue
		}
		if len(elfSym.Name) == 0 {
			continue
		}
		sect := rf.f.Sections[elfSym.Section]
		if sect.Type != elf.SHT_PROGBITS {
			continue
		}

		data := rf.symData(elfSym)

		goSym := &TargetSymbol{
			HostName: elfSym.Name,
			Size:     uint64(len(data)),
			P:        data,
		}

		if relocDatas, ok := rf.relocCache[elfSym.Section]; ok {
			for _, rd := range relocDatas {
				goSym.R = rf.processRelocations(rd.relocs, elfSym, goSym)
			}
		}

		symmap[goSym.HostName] = goSym
	}
	return symmap
}

func extractSymbolsFromELF(pctxt PatchContext, b []byte) map[string]*TargetSymbol {
	f, err := elf.NewFile(bytes.NewReader(b))
	if err != nil {
		panic(err)
	}
	elfSyms, err := f.Symbols() // SHT_SYMTAB
	if err != nil {
		panic(err)
	}
	rf := rawElfFile{f: f, symtab: elfSyms, pctxt: pctxt}
	rf.init()
	return rf.extractSymbolsFromELF()
}

// find a Go symbol for the reloc
// convert all reloc into PCREL;
func (rf *rawElfFile) convertElfReloc64(off uint64, info uint64, addend int64, elfSym elf.Symbol, goSym *TargetSymbol) TargetReloc {
	var reloc TargetReloc
	symNo := info >> 32
	t := elf.R_X86_64(info & 0xffff)
	reloc.Off = int32(off) - int32(elfSym.Value)
	var relocsym *elf.Symbol
	if symNo == 0 || symNo > uint64(len(rf.symtab)) {
		reloc.Sym = nil
	} else {
		relocsym = &rf.symtab[symNo-1]
		reloc.HostName = relocsym.Name
		if t == elf.R_X86_64_TPOFF32 {
			if relocsym.Name != "runtime.tls_g" {
				panic("the only tls we added to backend is runtime.tls_g")
			}
			reloc.Add = addend
			reloc.Type = objabi.R_TLS_LE
			reloc.Siz = 4
			return reloc
		}
		// bind := elf.ST_BIND(relocsym.Info)
		styp := elf.ST_TYPE(relocsym.Info)
		if relocsym.Section == elf.SHN_UNDEF {
			// should be an external symbol:
			// should be Go symbol only but currently llvm instrinsics will introduce some external symbol.
			reloc.Sym = rf.pctxt.LookupSym(relocsym.Name)
		} else if int(relocsym.Section) < len(rf.f.Sections) {
			relocsymsect := rf.f.Sections[relocsym.Section]
			switch {
			case relocsymsect.Flags&elf.SHF_EXECINSTR != 0:
				// must be a function and should be go function only
				reloc.Sym = rf.pctxt.LookupSym(relocsym.Name)
				// all function are external
				if styp != elf.STT_FUNC /*|| bind != elf.STB_GLOBAL*/ {
					panic("must be a function")
				}
			case relocsymsect.Type == elf.SHT_PROGBITS && relocsymsect.Flags&elf.SHF_WRITE == 0:
				// .rodata
				// can only be PC32 or 32 or 32S
				switch t {
				case elf.R_X86_64_PC32, elf.R_X86_64_32, elf.R_X86_64_32S:
					break
				default:
					panic(fmt.Errorf("TODO: unsupported reloc type: %s", t))
				}
				reloc.Sym = rf.sectionDataSym(relocsym.Section)
				if reloc.Sym == nil {
					panic("oops")
				}
				addend += int64(relocsym.Value)
			default:
				panic(fmt.Errorf("TODO: unsupported elf relocation symbol: %v", relocsym))
			}
		} else {
			panic(fmt.Errorf("TODO: unsupported elf relocation symbol: %v", relocsym))
		}
	}
	// see relSize in ldelf.go
	// see elfreloc1 in asm.go
	// see adddynrel in asm.go
	reloc.Siz = 4
	switch t {
	case elf.R_X86_64_PC32:
		reloc.Type = objabi.R_PCREL
		if reloc.Sym == nil {
			panic("TODO: cannot find symbol")
		}
		reloc.Add = addend + 4
	case elf.R_X86_64_PLT32:
		reloc.Type = objabi.R_CALL
		if reloc.Sym == nil {
			panic("TODO: cannot find symbol")
		}
		reloc.Add = addend + 4
	case elf.R_X86_64_32, elf.R_X86_64_32S:
		if reloc.Sym == nil {
			panic("TODO: we should convert a floating constant into a symbol")
		}
		reloc.Type = objabi.R_ADDR
		reloc.Add = addend
		if addend < 0 {
			panic(fmt.Errorf("expected >=0, got %d in %s for %s", addend, rf.pctxt.SymName(reloc.Sym), goSym.HostName))
		}
	case elf.R_X86_64_GOTPCREL, elf.R_X86_64_GOTPCRELX, elf.R_X86_64_REX_GOTPCRELX:
		reloc.Type = objabi.R_PCREL
		if reloc.Sym == nil {
			panic("TODO: GOT should be a Go symbol")
		}
		reloc.Add = addend + 4
	case elf.R_X86_64_GOT32:
		reloc.Type = objabi.R_ADDR
		if reloc.Sym == nil {
			panic("TODO: GOT should be a Go symbol")
		}
		sect := rf.f.Sections[elfSym.Section]
		if sect.Type != elf.SHT_PROGBITS {
			panic("must be rodata")
		}
		if sect.Flags&elf.SHF_WRITE != 0 {
			panic("must be rodata")
		}
		if sect.Flags&elf.SHF_EXECINSTR != 0 {
			panic("cannot be executable code")
		}
		reloc.Add = addend
		if addend < 0 {
			panic(fmt.Errorf("expected >=0, got %d in %s for %s", addend, rf.pctxt.SymName(reloc.Sym), goSym.HostName))
		}
	default:
		panic(fmt.Errorf("TODO: unsupported reloc type: %s", t))
	}
	return reloc
}
