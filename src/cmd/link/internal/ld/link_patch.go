// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.tangollvm && linux && amd64 && cgo && !compiler_bootstrap

package ld

import (
	"cmd/internal/goobj"
	"cmd/internal/hash"
	"cmd/internal/llvm"
	"cmd/internal/obj"
	"cmd/internal/objabi"
	"cmd/link/internal/loader"
	"cmd/link/internal/sym"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"internal/abi"
	"os"
	"reflect"
	"strings"
)

// Debug and logging configuration constants
const (
	debug                  = 0     // Debug verbosity level (0=off, 1=basic, 2=detailed, 3=verbose)
	printOldNewReloacation = false // Whether to print old vs new relocations for debugging
	printRelocationDiff    = false // Whether to print relocation differences
)

// debugPrint outputs debug information if the debug level is sufficient.
// This provides conditional logging based on the global debug level.
func debugPrint(level int, v ...interface{}) {
	if debug >= level {
		fmt.Println(v...)
	}
}

// mapDiff represents the differences between two string maps.
// Used for tracking changes in symbol relocations during patching.
type mapDiff struct {
	AddedKeys    []string             // Keys present in new but not old
	RemovedKeys  []string             // Keys present in old but not new
	ModifiedKeys map[string][2]string // Keys with different values: key -> [oldValue, newValue]
}

// diffMaps computes the differences between two string maps.
// This is used for debugging relocation changes during symbol patching.
func diffMaps(old, new map[string]string) mapDiff {
	diff := mapDiff{
		ModifiedKeys: make(map[string][2]string),
	}

	// Check for additions and modifications
	for k, v := range new {
		if oldV, exists := old[k]; exists {
			if !reflect.DeepEqual(v, oldV) {
				diff.ModifiedKeys[k] = [2]string{oldV, v}
			}
		} else {
			diff.AddedKeys = append(diff.AddedKeys, k)
		}
	}

	// Check for removals
	for k := range old {
		if _, exists := new[k]; !exists {
			diff.RemovedKeys = append(diff.RemovedKeys, k)
		}
	}

	return diff
}

// patchFuncData updates function metadata (FUNCDATA) for a given function symbol.
// FUNCDATA contains runtime information like garbage collection maps, stack object info, etc.
// This function replaces Go-generated FUNCDATA with LLVM-generated equivalents.
func (l *ldPatchContext) patchFuncData(f loader.Sym, id int, funcdata []loader.Sym, v *llvm.TargetSymbol, newsym func() loader.Sym) {
	ldr := l.loader

	// If both old and new data are empty, nothing to do
	if (id >= len(funcdata) || funcdata[id] == 0) && v == nil {
		return
	}

	// Validate that we have the expected funcdata slot
	if id >= len(funcdata) {
		// v is not nil
		panic("size should gen at object stage")
	} else {
		var item loader.Sym
		if funcdata[id] == 0 {
			// we have no idea to
			item = newsym()
		} else {
			item = ldr.GenAndCopyNewSym(funcdata[id])
			debugPrint(2, "Patch funcdata ", funcId2Name(id), item, v)
		}

		l.patchSymbol(item, v)

		// Update the function's FUNCDATA auxiliary table
		var tmp [8]*goobj.Aux
		funcauxs := ldr.DataAuxs(f, goobj.AuxFuncdata, tmp[:])
		funcauxs[id].SetSym(goobj.SymRef{PkgIdx: uint32(0), SymIdx: uint32(item)})
		funcdata[id] = item
	}
}

// funcId2Name converts FUNCDATA type IDs to human-readable names for debugging.
// These IDs correspond to different types of runtime metadata attached to functions.
func funcId2Name(id int) string {
	// Function data type constants from objabi package:
	// #define FUNCDATA_ArgsPointerMaps 0   /* garbage collector argument maps */
	// #define FUNCDATA_LocalsPointerMaps 1 /* garbage collector locals maps */
	// #define FUNCDATA_StackObjects 2      /* stack object information */
	// #define FUNCDATA_InlTree 3           /* inlining tree for debugger */

	switch id {
	case abi.FUNCDATA_ArgsPointerMaps:
		return "FUNCDATA_ArgsPointerMaps" // GC info for function arguments
	case abi.FUNCDATA_LocalsPointerMaps:
		return "FUNCDATA_LocalsPointerMaps" // GC info for local variables
	case abi.FUNCDATA_StackObjects:
		return "FUNCDATA_StackObjects" // Stack-allocated object metadata
	case abi.FUNCDATA_InlTree:
		return "FUNCDATA_InlTree" // Inlining information for debugger
	}
	return "unknown func id"
}

// pcId2Name converts PCDATA type IDs to human-readable names for debugging.
// PCDATA contains program-counter-indexed tables for runtime information.
func pcId2Name(id int) string {
	// PC data type constants from objabi package:
	// #define PCDATA_UnsafePoint 0     /* unsafe point information for GC */
	// #define PCDATA_StackMapIndex 1   /* index into stack maps */
	// #define PCDATA_InlTreeIndex 2    /* index into inlining tree */
	// #define PCDATA_ArgLiveIndex 3    /* argument liveness information */

	switch id {
	case abi.PCDATA_UnsafePoint:
		return "PCDATA_UnsafePoint" // Points where GC cannot run safely
	case abi.PCDATA_StackMapIndex:
		return "PCDATA_StackMapIndex" // Maps PC to stack layout info
	case abi.PCDATA_InlTreeIndex:
		return "PCDATA_InlTreeIndex" // Maps PC to inlining information
	case abi.PCDATA_ArgLiveIndex:
		return "PCDATA_ArgLiveIndex" // Tracks argument liveness
	}
	return "unknown pc id"
}

// patchPCData updates program-counter-indexed data (PCDATA) for a function.
// PCDATA tables map program counter values to various runtime information
// like garbage collection safe points, stack maps, etc.
func (l *ldPatchContext) patchPCData(f loader.Sym, id int, pcdata []loader.Sym, v *llvm.TargetSymbol) {
	if f == 0 {
		return // No function to patch
	}

	ldr := l.loader
	size := int64(v.Size)

	// Validate data consistency
	if size != int64(len(v.P)) {
		panic(fmt.Errorf("mismatched data size for %s, Size=%d, len(P)=%d", ldr.SymName(f), size, len(v.P)))
	}

	if id >= len(pcdata) {
		panic("size should gen at object stage")
	} else {
		if pcdata[id] == 0 {
			panic(fmt.Sprintf("pcdata[%d] is empty.", id))
		}

		// Create new symbol and update the function's PCDATA auxiliary table
		item := ldr.GenAndCopyNewSym(pcdata[id])
		pcdata[id] = item
		var tmp [8]*goobj.Aux
		pcauxs := ldr.DataAuxs(f, goobj.AuxPcdata, tmp[:])
		pcauxs[id].SetSym(goobj.SymRef{PkgIdx: uint32(0), SymIdx: uint32(item)})

		debugPrint(2, "Patch pcdata ", pcId2Name(id), item, v)
		l.patchSymbol(pcdata[id], v)
	}
}

// removePCData effectively removes a PCDATA entry by setting it to empty data.
// This is used when LLVM-compiled code doesn't need certain PC data that
// the Go compiler would normally generate.
func (l *ldPatchContext) removePCData(id int, pcdata []loader.Sym) {
	if id >= len(pcdata) {
		// PC data slot doesn't exist, nothing to remove
		return
	} else {
		// "Remove" by patching with nil (creates empty data)
		debugPrint(2, "remove pc data ", id, pcdata[id])
		l.patchSymbol(pcdata[id], nil)
	}
}

// patchSymbol replaces the content and relocations of a symbol with new data.
// This is the core function for updating symbols with LLVM-generated content
// while preserving Go-specific relocations that LLVM doesn't know about.
func (l *ldPatchContext) patchSymbol(s loader.Sym, v *llvm.TargetSymbol) {
	if s == 0 {
		return // Null symbol, nothing to patch
	}
	ldr := l.loader
	// Handle removal case: set symbol to empty
	if v == nil {
		sb := l.makeSymbolUpdater(s)
		sb.SetSize(0)
		sb.SetData([]byte{})
		sb.ResetRelocs()
		return
	}

	data := v.P
	name := ldr.SymName(s)

	// Maps for debugging relocation differences
	old, new := []string{}, []string{}
	oldSet, newSet := make(map[string]string, 0), make(map[string]string, 0)

	// Step 1: Update symbol data with LLVM-generated content
	sb := l.makeSymbolUpdater(s)
	sb.SetSize(int64(len(data)))
	sb.SetData(data)

	// Step 2: Preserve Go-specific relocation types that LLVM doesn't generate
	// These are used by the Go runtime for type system operations
	sb.SortRelocs()
	relocs := sb.Relocs()

	debugPrint(2, ldr.SymName(s), "- old")

	// Storage for preserving Go-specific relocations
	var tmp []llvm.TargetReloc
	// Scan existing relocations and preserve Go-specific ones
	for i := 0; i < relocs.Count(); i++ {
		re := relocs.At(i)
		cul := fmt.Sprintln("\t", ldr.SymName(re.Sym()), re.Off(), re.Siz(), re.Type())
		old = append(old, cul)
		oldSet[ldr.SymName(re.Sym())] = cul
		debugPrint(3, cul)

		// Preserve specific Go relocation types that LLVM won't generate
		switch re.Type() {
		case objabi.R_USEFIELD, // Field usage for unused field elimination
			objabi.R_USEIFACE,       // Interface usage tracking
			objabi.R_USETYPE,        // Type usage tracking
			objabi.R_USEIFACEMETHOD, // Interface method usage
			objabi.R_USENAMEDMETHOD: // Generic interface method usage
			tmp = append(tmp, llvm.TargetReloc{
				Off:  re.Off(),
				Siz:  re.Siz(),
				Type: re.Type(),
				Add:  re.Add(),
				Sym:  l.toTargetSym(re.Sym()),
			})
		}
	}

	// Step 3: Merge preserved Go relocations with new LLVM relocations
	sb.ResetRelocs()
	tmpLen := len(tmp)
	relocs = sb.AddRelocs(len(tmp) + len(v.R))

	if debug > 3 {
		fmt.Println(ldr.SymName(s))
	}

	// Add preserved Go relocations first
	for i := range tmp {
		re := relocs.At(i)
		j := &tmp[i]
		re.Set(j.Off, j.Siz, uint16(j.Type), j.Add, goobj.SymRef{PkgIdx: 0, SymIdx: uint32(l.toLoaderSym(j.Sym))})
		debugPrint(3, "\t", ldr.SymName(re.Sym()), re.Off(), re.Siz(), re.Type())
	}

	// Add new LLVM-generated relocations
	for i, j := range v.R {
		re := relocs.At(tmpLen + i)
		re.Set(j.Off, j.Siz, uint16(j.Type), j.Add, goobj.SymRef{PkgIdx: 0, SymIdx: uint32(l.toLoaderSym(j.Sym))})
		debugPrint(3, "\t", ldr.SymName(re.Sym()), re.Off(), re.Siz(), re.Type())
	}

	// Sort relocations by offset for efficient lookup
	sb.SortRelocs()

	// Build new relocation map for debugging
	for i := 0; i < relocs.Count(); i++ {
		re := relocs.At(i)
		cul := fmt.Sprintln("\t", ldr.SymName(re.Sym()), re.Off(), re.Siz(), re.Type())
		new = append(new, cul)
		newSet[ldr.SymName(re.Sym())] = cul
	}

	// Debug output for relocation changes
	if printOldNewReloacation {
		fmt.Println(name, " -old")
		fmt.Println(old)
		fmt.Println(name, " -new")
		fmt.Println(new)
	}

	if !printRelocationDiff {
		return
	}

	// Print detailed relocation differences for debugging
	diff := diffMaps(oldSet, newSet)
	fmt.Println(name)
	if len(diff.AddedKeys) > 0 {
		fmt.Println("Add: ")
		for _, v := range diff.AddedKeys {
			fmt.Println(v)
		}
	}

	if len(diff.RemovedKeys) > 0 {
		fmt.Println("Remove: ")
		for _, v := range diff.RemovedKeys {
			fmt.Println(v)
		}
	}

	if len(diff.ModifiedKeys) > 0 && debug > 3 {
		fmt.Println("Modified: ")
		for _, v := range diff.ModifiedKeys {
			fmt.Println(v)
		}
	}
}

func (l *ldPatchContext) checkPatchedTextSym(v *llvm.TargetSymbol) error {
	if v == nil {
		return nil
	}

	if err := l.checkPatchedSym(v); err != nil {
		if l.Debugvlog > 0 {
			fmt.Println("Fail to check symbol:", v.HostName, err.Error())
		}
		return err
	}

	auxs := l.p.Auxs(v.HostName)
	for _, aux := range auxs {
		if err := l.checkPatchedSym(aux); err != nil {
			if l.Debugvlog > 0 {
				fmt.Println("Fail to check symbol aux:", aux.HostName, err.Error())
			}
			return err
		}
	}

	return nil
}

// checkPatchedSym validates that all relocations in a symbol have valid targets.
// This helps catch errors where LLVM-generated symbols reference non-existent
// or incorrectly mapped Go symbols.
func (l *ldPatchContext) checkPatchedSym(v *llvm.TargetSymbol) error {
	if v == nil {
		return nil
	}
	// Verify all relocations have valid symbol references
	for i := range v.R {
		r := &v.R[i]
		if r.Type == objabi.R_TLS_LE {
			// TLS_LE has no symbol
			continue
		}
		if r.Sym == nil {
			if r.HostName != "" {
				s := l.findSym(r.HostName)
				if s != 0 {
					r.Sym = l.toTargetSym(s)
					if debug > 0 {
						fmt.Println("Found missing symbol for ", v.HostName, r.HostName, l.loader.SymName(s))
					}
				}
			}
			if r.Sym == nil {
				return fmt.Errorf("%s #%d reloc (%s) has no symbol", v.HostName, i, r.HostName)
			}
		}
		if r.Type == 0 {
			return fmt.Errorf("%s #%d reloc (%s:%s) has no type", v.HostName, i, l.SymName(r.Sym), r.HostName)
		}
	}
	return nil
}

type targetSym loader.Sym

func (s *targetSym) SymId() int64 {
	if s == nil {
		return 0
	}
	return int64(*s)
}

type ldPatchContext struct {
	*Link

	p llvm.Patcher

	// some internal symbols are not added to the loader named map
	nametab map[string]loader.Sym

	// for conversion between llvm.TargetSym and loader.Sym
	symcache map[loader.Sym]*targetSym

	globalFileTable []FileEntry
	localFileTable  map[*sym.CompilationUnit]map[string]int

	// In strictMode, all symbols should be avaiable.
	// This mode is only used fo debugging. In theory, LTO may do more
	// or less optimization the normal build. The symbol sets are not necessarily
	// to be exactly the same.
	strictMode bool
}

func (l *ldPatchContext) Init(p llvm.Patcher) {
	l.p = p
}

func (l *ldPatchContext) prepareGlobalFileTable(p llvm.Patcher) {
	if l.globalFileTable != nil {
		return
	}

	b := p.EnsureSymbol("tangollvmlto..filetable")
	globalFileTable, err := ReadGoFileTabFromBytes(b.P)
	if err != nil {
		panic(err)
	}
	l.globalFileTable = globalFileTable
}

func (l *ldPatchContext) prepareLocalFileTable(cu *sym.CompilationUnit) map[string]int {
	// Update file table mappings for the compilation unit
	if f, ok := l.localFileTable[cu]; ok {
		return f
	}
	filetable := make(map[string]int, 0)
	// Build map of existing file table entries
	for i, v := range cu.FileTable {
		filetable[v] = i
	}
	l.localFileTable[cu] = filetable
	return filetable
}

func (l *ldPatchContext) init() {
	if os.Getenv("GOLLVMLTOSTRICTMODE") == "1" {
		l.strictMode = true
	}
	ldr := l.loader
	nsym := loader.Sym(ldr.NSym())
	l.symcache = make(map[loader.Sym]*targetSym, nsym)
	l.localFileTable = make(map[*sym.CompilationUnit]map[string]int, len(l.LibraryByPkg))

	l.nametab = make(map[string]loader.Sym, nsym)

	addSymToTab := func(name string, ver int, s loader.Sym) {
		if name == "" {
			return
		}
		// keep internal has prority
		if e, ok := l.nametab[name]; ok {
			if e == 0 {
				return
			}
			// other conflict is not allowed
			ever := ldr.SymVersion(e)
			if ever == ver {
				// is it possible?
				// same version and same name?
				return
			}
			if ver >= sym.SymVerStatic {
				if ever >= sym.SymVerStatic {
					// report error
					//panic(fmt.Errorf("two static symbols have the same name [%s]" +
					//	"but different version [%d] vs [%d]", name, ever, sver))
					// mark conflict
					l.nametab[name] = 0
					return
				}
				// static can safely override non-static
			} else {
				// sver must be ABI0 or ABIInternal
				if ever >= sym.SymVerStatic {
					// existing symbol is static
					return
				}
				// ever must be ABI0 or ABIInternal
				// override only if existing is ABI0 and new is ABIInternal
				// i.e., ever < sver
				if !(ever == sym.SymVerABI0 && ver == sym.SymVerABIInternal) {
					return
				}
			}
		}
		if l.Debugvlog > 0 {
			fmt.Println("register", name, "to", s, "in name table")
		}
		l.nametab[name] = s
	}

	ldr.IterateAllGoSyms(addSymToTab)
}

func (l *ldPatchContext) toLoaderSym(t llvm.TargetSym) loader.Sym {
	if t == nil {
		return 0
	}
	return loader.Sym(*(t.(*targetSym)))
}

func (l *ldPatchContext) toTargetSym(s loader.Sym) llvm.TargetSym {
	if s == 0 {
		return nil
	}
	if c, ok := l.symcache[s]; ok {
		return c
	}
	c := (*targetSym)(&s)
	l.symcache[s] = c
	return c
}

// iterate each text symbol
func (l *ldPatchContext) ForEachTextSym(f func(pctxt llvm.PatchContext, tsym llvm.TargetSym, tsymbol *llvm.TargetSymbol)) {
	// prepare the global file table
	l.prepareGlobalFileTable(l.p)

	ldr := l.loader
	nsym := loader.Sym(ldr.NSym())
	count := 0
	// Process each symbol that has corresponding LLVM-generated data
	for k := loader.Sym(1); k < nsym; k++ {
		if ldr.SymType(k) != sym.STEXT {
			continue
		}

		// Skip duplicate symbols
		if ldr.AttrDuplicateOK(k) {
			continue
		}

		name := ldr.SymName(k)
		reqABI, ok := sym.VersionToABI(ldr.SymVersion(k))
		if !ok {
			continue
		}
		llvmname := llvm.EncodeFuncNameAndABI(name, reqABI)

		tsymbol := l.p.Symbol(llvmname)
		if tsymbol == nil {
			continue
		}

		if err := l.checkPatchedTextSym(tsymbol); err != nil {
			continue
		}

		if ldr.IsExternal(k) {
			panic("only llvm can modify goobj symbols")
		}

		// do the patch work
		f(l, l.toTargetSym(k), tsymbol)

		count += 1

		// Debug output showing final state
		if debug > 2 {
			fmt.Println("After Patch: ")
			fmt.Println("PCDATA: ")
			var pcdatatmp [8]loader.Sym
			pcdata := ldr.Pcdata(k, pcdatatmp[:])
			for i, item := range pcdata {
				if item == 0 {
					fmt.Println(i, "nil 0")
					continue
				}
				fmt.Println(i, item, " ", ldr.Data(item))
			}

			var funcdatatmp [8]loader.Sym
			data := ldr.Funcdata(k, funcdatatmp[:])
			fmt.Println("FUNCDATA: ")
			for i, item := range data {
				if item == 0 {
					fmt.Println(i, "nil 0")
					continue
				}
				fmt.Println(i, item, " ", ldr.Data(item))
			}
		}
	}

	if l.Debugvlog > 0 {
		fmt.Println("patched text symbol:", count)
	}
}

func (l *ldPatchContext) PatchSymbol(tsym llvm.TargetSym, v *llvm.TargetSymbol) {
	s := l.toLoaderSym(tsym)
	l.patchSymbol(s, v)
}

func (l *ldPatchContext) PatchFuncInfo(tsym llvm.TargetSym, fisymbol *llvm.TargetSymbol) {
	// Update symbol references in function info data
	for _, v := range fisymbol.R {
		if v.Sym == nil {
			continue
		}
		// Encode symbol reference as pkg index (0) + symbol index
		binary.LittleEndian.PutUint32(fisymbol.P[v.Off:], uint32(0))
		binary.LittleEndian.PutUint32(fisymbol.P[v.Off+4:], uint32(l.toLoaderSym(v.Sym)))
	}

	ldr := l.loader
	s := l.toLoaderSym(tsym)
	funcinfo := ldr.FuncInfo(s)
	funcinfo.Preload()

	// Update the function info symbol data
	funcinfotarget := funcinfo.FuncInfoSym()
	sb := l.makeSymbolUpdater(funcinfotarget)
	sb.SetSize(int64(len(fisymbol.P)))
	sb.SetData(fisymbol.P)

	debugPrint(3, "patch funcinfo ", funcinfotarget)
	debugPrint(3, "1 check inl tree nodes -> ", funcinfo.NumInlTree(), funcinfo.NumFile())

	// Reload function info after patching
	funcinfo = ldr.FuncInfo(s)
	funcinfo.Preload()
	debugPrint(3, "2 check inl tree nodes -> ", funcinfo.NumInlTree(), funcinfo.NumFile())

	if debug >= 3 {
		for i := uint32(0); i < funcinfo.NumInlTree(); i++ {
			r := funcinfo.InlTree(int(i))
			fmt.Println(r.Func, r)
		}
	}

	// Update file table mappings for the compilation unit
	cu := ldr.SymUnit(s)
	filetable := l.prepareLocalFileTable(cu)

	// Get function info structure layout
	funinfolengths := (*goobj.FuncInfo)(nil).ReadFuncInfoLengths(fisymbol.P)

	// Update file indices in function info
	for i := uint32(0); i < funcinfo.NumFile(); i++ {
		r := funcinfo.File(int(i))
		globalentry := l.globalFileTable[int(r)]
		fullname := "gofile.." + globalentry.FullPath()

		// Add to file table if not already present
		originalIndex, ok := filetable[fullname]
		if !ok {
			originalIndex = len(filetable)
			filetable[fullname] = originalIndex
			cu.FileTable = append(cu.FileTable, fullname)
		}

		// Update file index in function info data
		binary.LittleEndian.PutUint32(fisymbol.P[funinfolengths.FileOff+i*4:], uint32(originalIndex))
	}

	// Update file indices in inlining tree
	for i := uint32(0); i < funcinfo.NumInlTree(); i++ {
		r := funcinfo.InlTree(int(i))
		oldIndex := int(r.File)
		globalentry := l.globalFileTable[oldIndex]
		originalIndex, ok := filetable["gofile.."+globalentry.FullPath()]
		if !ok {
			panic("error in search originalIndex")
		}

		// Update file index in inlining tree node
		const inlTreeNodeSize = 4 * 6
		binary.LittleEndian.PutUint32(fisymbol.P[funinfolengths.InlTreeOff+i*inlTreeNodeSize+4:], uint32(originalIndex))
	}

	// Finalize function info updates
	sb.SetSize(int64(len(fisymbol.P)))
	sb.SetData(fisymbol.P)

	funcinfo = ldr.FuncInfo(s)
	funcinfo.Preload()
}

func (l *ldPatchContext) updatePCData(b []byte, update func(val int32) int32) []byte {
	pcfileIt := obj.NewPCIter(uint32(l.Arch.MinLC))
	dst := make([]byte, 0, len(b))
	buf := make([]byte, binary.MaxVarintLen32)
	started := false
	val := int32(-1)
	oldval := val
	pc := uint32(0)
	for pcfileIt.Init(b); !pcfileIt.Done; pcfileIt.Next() {
		// Update val. If it's not changing, keep going.
		val = update(pcfileIt.Value)

		if started {
			pcdelta := (pcfileIt.PC - pc) / pcfileIt.PCScale
			n := binary.PutUvarint(buf, uint64(pcdelta))
			dst = append(dst, buf[:n]...)
			pc = pcfileIt.PC
		}

		delta := val - oldval
		n := binary.PutVarint(buf, int64(delta))
		dst = append(dst, buf[:n]...)
		oldval = val
		started = true
	}

	if started {
		v := (pcfileIt.PC - pc) / pcfileIt.PCScale
		n := binary.PutUvarint(buf, uint64(v))
		dst = append(dst, buf[:n]...)
		// add terminating varint-encoded 0, which is just 0
		dst = append(dst, 0)
		return dst
	} else {
		// no need to update empty pcfile
		return b
	}
}

// Keep the signature the same as other place
func (l *ldPatchContext) PatchPCData(tsym llvm.TargetSym, pcspsym, pcfilesym, pclinesym, pcinlinesym *llvm.TargetSymbol, pcdatasyms []*llvm.TargetSymbol) {
	ldr := l.loader
	f := l.toLoaderSym(tsym)
	var pcdatatmp [8]loader.Sym
	pcsp, pcfile, pcline, pcinline, pcdata := ldr.PcdataAuxs(f, pcdatatmp[:])

	if pcspsym != nil {
		debugPrint(2, "patch pcsp -> ", pcsp, pcspsym, ldr.IsExternal(pcsp))
		pcsp = ldr.GenAndCopyNewSym(pcsp)
		l.patchSymbol(pcsp, pcspsym)
		aux := ldr.GoAux(f, goobj.AuxPcsp)
		aux.SetSym(goobj.SymRef{PkgIdx: uint32(0), SymIdx: uint32(pcsp)})
	}

	if pclinesym != nil {
		// newpcline := ldr.GenAndCopyNewSym(pcline)
		debugPrint(2, "patch pcline -> ", pcline, pclinesym)
		pcline = ldr.GenAndCopyNewSym(pcline)
		l.patchSymbol(pcline, pclinesym)
		aux := ldr.GoAux(f, goobj.AuxPcline)
		aux.SetSym(goobj.SymRef{PkgIdx: uint32(0), SymIdx: uint32(pcline)})
	}

	if pcinlinesym != nil {
		// newpcinline := ldr.GenAndCopyNewSym(pcinline)
		debugPrint(2, "patch pcinline -> ", pcinline, pcinlinesym)
		pcinline = ldr.GenAndCopyNewSym(pcinline)
		l.patchSymbol(pcinline, pcinlinesym)
		aux := ldr.GoAux(f, goobj.AuxPcinline)
		aux.SetSym(goobj.SymRef{PkgIdx: uint32(0), SymIdx: uint32(pcinline)})
	}

	if pcfilesym != nil {
		filetable := l.prepareLocalFileTable(ldr.SymUnit(f))
		updateFileIndex := func(val int32) int32 {
			if val != -1 {
				globalentry := l.globalFileTable[int(val)]
				originalIndex := filetable["gofile.."+globalentry.FullPath()]
				val = int32(originalIndex)
				if val == -1 {
					panic("cannot use -1 as an existing file index")
				}
			}
			return val
		}
		// Use the following code to validate PCData encoding
		//{
		//	b := l.updatePCData(pcfilesym.P, func(val int32) int32 {
		//		return val
		//	})
		//	if !bytes.Equal(b, pcfilesym.P) {
		//		panic("invalid encoding")
		//	}
		//}
		pcfilesym.P = l.updatePCData(pcfilesym.P, updateFileIndex)
		debugPrint(2, "patch pcfile -> ", pcfile, pcfilesym)
		pcfile = ldr.GenAndCopyNewSym(pcfile)
		l.patchSymbol(pcfile, pcfilesym)
		aux := ldr.GoAux(f, goobj.AuxPcfile)
		aux.SetSym(goobj.SymRef{PkgIdx: uint32(0), SymIdx: uint32(pcfile)})
	}

	pcunsafesym := pcdatasyms[abi.PCDATA_UnsafePoint]
	pcstackmapsym := pcdatasyms[abi.PCDATA_StackMapIndex]
	pcarglivesym := pcdatasyms[abi.PCDATA_ArgLiveIndex]

	// Unsafe point information (where GC cannot run)
	if pcunsafesym != nil {
		l.patchPCData(f, abi.PCDATA_UnsafePoint, pcdata, pcunsafesym)
	}

	pcdata = ldr.Pcdata(f, pcdata)
	if pcstackmapsym != nil {
		l.patchPCData(f, abi.PCDATA_StackMapIndex, pcdata, pcstackmapsym)
	}

	pcdata = ldr.Pcdata(f, pcdata)
	if pcarglivesym != nil {
		// TODO: it is nil right now but we still keep the code
		// because we will support ArgLiveInfo soon
		l.patchPCData(f, abi.PCDATA_ArgLiveIndex, pcdata, pcarglivesym)
	} else {
		l.removePCData(abi.PCDATA_ArgLiveIndex, pcdata)
	}
}

func (l *ldPatchContext) PatchFuncData(fsym llvm.TargetSym, fdid int, fdsymbol *llvm.TargetSymbol) {
	f := l.toLoaderSym(fsym)
	ldr := l.loader
	name := ldr.SymName(f)

	var tmp [8]loader.Sym
	data := ldr.EnsureFuncdata(f, tmp[:], fdid)
	if fdid == abi.FUNCDATA_StackObjects {
		if fdsymbol != nil {
			// Convert R_ADDR relocations to R_ADDROFF for stack objects
			for i := range fdsymbol.R {
				r := &fdsymbol.R[i]
				if r.Type != objabi.R_ADDR {
					panic("must be R_ADDR")
				}
				r.Type = objabi.R_ADDROFF
			}

			// we will patch objabi.FUNCDATA_StackObjects
			// try to ensure
			stkname := name + ".stkobj"
			x := l.findSym(stkname)
			y := data[abi.FUNCDATA_StackObjects]
			if x != 0 {
				// x and y can be both 0
				// .stkobj symbol is unique
				if y != x {
					if y != 0 {
						panic("stack objects are not unique")
					}
					// stkname may exist but not in the function table
					// because LLVM will remove it during compiler patching.
				}
			}
			newsym := func() loader.Sym {
				// *.stkobj are ABI0 static symbols
				if !ldr.IsExternal(f) {
					// f is external and its SymVersion is package localSymVersion
					panic("f must b external to modify")
				}
				// TODO: stackobjects are static symbols
				if _, ok := l.nametab[stkname]; ok {
					// stkobj may be removed by llvm_patcher
					return l.nametab[stkname]
				}
				nsb := ldr.MakeSymbolBuilder(stkname)
				nsb.SetType(sym.SRODATA)
				l.nametab[stkname] = nsb.Sym()
				return nsb.Sym()
			}
			l.patchFuncData(f, abi.FUNCDATA_StackObjects, data, fdsymbol, newsym)
		} else {
			// Clear stack objects if LLVM didn't generate any
			l.patchFuncData(f, abi.FUNCDATA_StackObjects, data, nil, nil)
		}
		return
	}

	if fdid == abi.FUNCDATA_ArgsPointerMaps || fdid == abi.FUNCDATA_LocalsPointerMaps {
		if fdsymbol == nil {
			// TODO: we cannot remove the symbol
			// We need to emit an empty map instead.
		} else {
			sum := hash.Sum32(fdsymbol.P)
			str := base64.StdEncoding.EncodeToString(sum[:16])
			symname := fmt.Sprintf("gclocals·%s", str)

			if data[fdid] == 0 {
				ds := l.findSym(symname)
				if ds != 0 {
					data[fdid] = ds
					return
				}
			}

			newgcsymbol := func() loader.Sym {
				if !ldr.IsExternal(f) {
					// f is external and its SymVersion is package localSymVersion
					panic("f must b external to modify")
				}
				// TODO: GCData are static symbols
				nsb := ldr.MakeSymbolBuilder(symname)
				nsb.SetType(sym.SRODATA)
				nsb.SetLocal(true)
				nsb.SetDuplicateOK(true)
				if _, ok := l.nametab[symname]; ok {
					panic("found a not found symbol " + symname)
				}
				l.nametab[symname] = nsb.Sym()
				return nsb.Sym()
			}

			l.patchFuncData(f, fdid, data, fdsymbol, newgcsymbol)
		}
		return
	}

	panic(fmt.Errorf("unknown funcdata id: %d", fdid))
}

func (l *ldPatchContext) SymName(sym llvm.TargetSym) string {
	s := l.toLoaderSym(sym)
	return l.loader.SymName(s)
}

func (l *ldPatchContext) SymPkg(sym llvm.TargetSym) string {
	s := l.toLoaderSym(sym)
	return l.loader.SymPkg(s)
}

func (l *ldPatchContext) SymSize(sym llvm.TargetSym) int64 {
	s := l.toLoaderSym(sym)
	return l.loader.SymSize(s)
}

func (l *ldPatchContext) SymAttrNoSplit(sym llvm.TargetSym) bool {
	s := l.toLoaderSym(sym)
	return l.loader.IsNoSplit(s)
}

func (l *ldPatchContext) LookupSym(name string) llvm.TargetSym {
	s := l.FindSym(name)
	if s == nil {
		if l.strictMode {
			panic("cannot find symbol " + name)
		} else {
			if l.Debugvlog > 0 {
				fmt.Println("LookupSym: cannot find symbol " + name)
			}
		}
	}
	return s
}

func (l *ldPatchContext) findSym(name string) loader.Sym {
	var s loader.Sym
	n, a := llvm.DecodeFuncNameAndABI(name)
	ldr := l.loader

	if a == obj.ABI0 {
		// only ABI0 function symbols can be encoded with ABI0
		// Data symbols should not be encoded as ABI0
		s = ldr.Lookup(n, sym.ABIToVersion(obj.ABI0))
		if s != 0 {
			t := ldr.SymType(s)
			if t != sym.STEXT && t != sym.STEXTFIPS {
				if l.strictMode {
					panic("an ABI0 symbol must be a function symbol: " + name + ":" + t.String())
				}
			}
			return s
		}
		// stop finding the symbol because ABI0 here is exact.
		// panic("cannot find ABIO function symbol " + name)
		return s
	}

	// a must be ABIInternal but name may be an ABI0 or static data symbol
	// Let's try to find it with ABIInternal first
	s = ldr.Lookup(n, sym.ABIToVersion(obj.ABIInternal))
	if s != 0 {
		// Here if n is an ABIInternal symbol, it cannot be a static symbol
		// in LLVM at the same time. Because we cannot distinguish ABIInternal
		// and ABI
		t := ldr.SymType(s)
		if t != sym.STEXT && t != sym.STEXTFIPS {
			if l.strictMode {
				panic("an ABIInternal symbol must be a function symbol: " + name)
			}
		} else {
			return s
		}
	}

	// search in named map
	// There are three kinds of symbols
	// ABI0: both data and function (i.e., text) can be ABI0
	// ABIInternal: only function can be ABIInternal
	// STATIC: GC data or linker created symbol for ELF and MachO
	s, ok := l.nametab[name]
	if ok {
		return s
	}

	createGCBits := func(symname string, b []byte) loader.Sym {
		sb := ldr.MakeSymbolBuilder(symname)
		sb.SetData(b)
		sb.SetSize(int64(len(b)))
		sb.SetLocal(true)
		sb.SetType(sym.SRODATA)
		sb.SetDuplicateOK(true)
		l.nametab[symname] = sb.Sym()
		return sb.Sym()
	}

	if strings.HasPrefix(name, "runtime.gcbits.") {
		p := name[15:]
		ptrmask, err := hex.DecodeString(p)
		if err == nil {
			// symname := fmt.Sprintf("runtime.gcbits.%x", ptrmask)
			// fmt.Println("runtime.gcbits: 1", symname, name, ptrmask)
			return createGCBits(name, ptrmask)
		} else {
			panic("cannot decode gcbits: " + err.Error())
		}
	}

	if l.p != nil {
		// go.rodata is introduced by TangoLLVM to embedding zero-relocation RO data
		if ts := l.p.Symbol("go.rodata." + name); ts != nil {
			if len(ts.P) >= 4 {
				// See GoAuxDataHandler.cpp for encoding
				c1, c2, c3, c4 := uint64(ts.P[0]), uint64(ts.P[1]), uint64(ts.P[2]), uint64(ts.P[3])
				length := int64(c1 + c2<<8 + c3<<16 + c4<<24)
				if int64(len(ts.P)) == length+4 {
					if length == 0 {
						return l.findSym("runtime.zerobase")
					}
					symname := fmt.Sprintf("runtime.gcbits.%x", ts.P[4:])
					if s, ok = l.nametab[symname]; ok {
						return s
					}
					// ptrmask, _ := hex.DecodeString(symname[15:])
					// fmt.Println("runtime.gcbits.2", name, symname, ptrmask, ts.P)
					return createGCBits(symname, ts.P[4:])
				} // else report error
			}
		}
	}

	return 0
}

func (l *ldPatchContext) FindSym(name string) llvm.TargetSym {
	s := l.findSym(name)
	if s == 0 {
		return nil
	}
	return l.toTargetSym(s)
}

func (l *ldPatchContext) LookupTypeSym(tyid string) llvm.TargetSym {
	tysym := l.findSym("type:" + tyid)
	if tysym == 0 {
		tysym = l.findSym("type:noalg." + tyid)
	}
	if tysym == 0 {
		if l.strictMode {
			panic("cannot find type via id " + tyid)
		}
	}
	return l.toTargetSym(tysym)
}

func (l *ldPatchContext) makeSymbolUpdater(s loader.Sym) *loader.SymbolBuilder {
	ldr := l.loader

	isExternal := ldr.IsExternal(s)
	name := ldr.SymName(s)
	pkg := ldr.SymPkg(s)
	size := ldr.SymSize(s)
	flag := ldr.SymAttr(s)
	typ := ldr.SymType(s)
	ver := ldr.SymVersion(s)
	cu := ldr.SymUnit(s)
	isFromAsm := ldr.IsFromAssembly(s)
	isDupOK := ldr.AttrDuplicateOK(s)
	isReadOnly := ldr.AttrReadOnly(s)
	isDict := ldr.IsDict(s)
	isItab := ldr.IsItab(s)
	nAux := ldr.NAux(s)

	// normally, a symbol updater does not modify aux
	sb := ldr.MakeSymbolUpdater(s)
	ldr.MakeMutableForLTO(s)

	assertTrue := func(lhs, rhs interface{}) {
		if lhs != rhs {
			panic(fmt.Errorf("not equal: %v vs %v", lhs, rhs))
		}
	}

	if !isExternal {
		// we must ensure all these Goobj properties preseved after
		// make the symbol an external symbol.
		assertTrue(name, ldr.SymName(s))
		assertTrue(pkg, ldr.SymPkg(s))
		assertTrue(size, ldr.SymSize(s))
		assertTrue(flag, ldr.SymAttr(s))
		assertTrue(typ, ldr.SymType(s))
		assertTrue(ver, ldr.SymVersion(s))
		assertTrue(cu, ldr.SymUnit(s))
		assertTrue(isFromAsm, ldr.IsFromAssembly(s))
		assertTrue(isDupOK, ldr.AttrDuplicateOK(s))
		assertTrue(isReadOnly, ldr.AttrReadOnly(s))
		assertTrue(isDict, ldr.IsDict(s))
		assertTrue(isItab, ldr.IsItab(s))
		assertTrue(nAux, ldr.NAux(s))
	}

	return sb
}

func (l *ldPatchContext) CreateConstDataSymbol(data []byte, size int64, align int32) llvm.TargetSym {
	ldr := l.loader
	name := fmt.Sprintf("gopatchdata:%x", md5.Sum(data))
	sb := ldr.MakeSymbolBuilder(name)
	sb.SetType(sym.SRODATA)
	sb.SetDuplicateOK(true)
	sb.SetData(data)
	sb.SetSize(size)
	sb.SetAlign(align)
	sb.SetLocal(true)
	return l.toTargetSym(sb.Sym())
}

// patchSymbols is the main entry point for patching LLVM-compiled symbols.
// This function reads LLVM-generated symbol data and patches the corresponding
// Go symbols with updated code, data, and metadata.
func patchSymbols(ctxt *Link) {
	// RunLTO communicates the per-link output path through this process-local
	// environment variable. It is an internal handoff, not user configuration.
	lto := os.Getenv("LTO_BIN")
	if lto == "" {
		return // No LLVM-generated code to patch
	}

	// Read the LLVM-generated ELF file containing symbol data
	b, err := os.ReadFile(lto)
	if err != nil {
		Exitf("read TangoLLVM LTO output %q: %v", lto, err)
	}

	pctxt := &ldPatchContext{
		Link: ctxt,
	}

	pctxt.init()

	llvm.Patch(pctxt, b, ctxt.Debugvlog)
}
