// Copyright 2009 The Go Authors. All rights reserved.
// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Portions of lookupGCSym are adapted from dgcptrmask in Go
// src/cmd/compile/internal/reflectdata/reflect.go. These portions and their
// modifications retain BSD 3-Clause terms in LICENSE_UPSTREAM.
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2025-06-26.

//go:build goexperiment.tangollvm && linux && amd64 && cgo && !compiler_bootstrap

package ssagen

import (
	"cmd/compile/internal/base"
	"cmd/compile/internal/objw"
	"cmd/compile/internal/typecheck"
	"cmd/compile/internal/types"
	"cmd/internal/goobj"
	"cmd/internal/llvm"
	"cmd/internal/obj"
	"cmd/internal/objabi"
	"cmd/internal/src"
	"encoding/hex"
	"fmt"
	"internal/abi"
	"internal/buildcfg"
	"strconv"
	"strings"
	"unsafe"
)

type targetSym obj.LSym

func (t *targetSym) SymId() int64 {
	return int64(uintptr(unsafe.Pointer(t)))
}

func toTargetSym(l *obj.LSym) llvm.TargetSym {
	if l == nil {
		return nil
	}
	return (*targetSym)(l)
}

func toLSym(t llvm.TargetSym) *obj.LSym {
	if t == nil {
		return nil
	}
	return (*obj.LSym)(t.(*targetSym))
}

// PatchContext for the go compiler (gc)
type gcPatchCtxt struct {
	p llvm.Patcher
}

func (c *gcPatchCtxt) Init(p llvm.Patcher) {
	c.p = p
}

func (c *gcPatchCtxt) ForEachTextSym(fn func(pctxt llvm.PatchContext, tsym llvm.TargetSym, tsymbol *llvm.TargetSymbol)) {
	for _, f := range base.Ctxt.Text {
		tsym := toTargetSym(f)
		llvmname := llvm.EncodeFuncNameAndABI(f.Name, f.ABI())
		tsymbol := c.p.Symbol(llvmname)
		if tsymbol == nil {
			continue
		}
		fn(c, tsym, tsymbol)
	}
}

func convertReloc(tr llvm.TargetReloc) obj.Reloc {
	return obj.Reloc{
		Off:  tr.Off,
		Siz:  tr.Siz,
		Type: tr.Type,
		Add:  tr.Add,
		Sym:  toLSym(tr.Sym),
	}
}

func (c *gcPatchCtxt) PatchSymbol(tsym llvm.TargetSym, symbol *llvm.TargetSymbol) {
	lsym := toLSym(tsym)
	patchSymbol(lsym, symbol)
}

func (c *gcPatchCtxt) PatchFuncInfo(tsym llvm.TargetSym, fisymbol *llvm.TargetSymbol) {
	f := toLSym(tsym)
	fninfo := f.Func()
	isym := &obj.LSym{
		Type:   objabi.SDATA, // for now, I don't think it matters
		PkgIdx: goobj.PkgIdxSelf,
		SymIdx: 0,
		P:      fisymbol.P,
		R:      make([]obj.Reloc, 0, len(fisymbol.R)),
	}
	for _, r := range fisymbol.R {
		isym.R = append(isym.R, convertReloc(r))
		if r.Sym != nil {
			fninfo.InlineTreeLSym = append(fninfo.InlineTreeLSym, toLSym(r.Sym))
		}
	}
	fninfo.FuncInfoSym = isym
}

// Keep the signature the same as other place
func (c *gcPatchCtxt) PatchPCData(tsym llvm.TargetSym, pcsp, pcfile, pcline, pcinline *llvm.TargetSymbol, pcdata []*llvm.TargetSymbol) {
	f := toLSym(tsym)
	fninfo := f.Func()
	pcstackmap := pcdata[abi.PCDATA_StackMapIndex]
	pcunsafe := pcdata[abi.PCDATA_UnsafePoint]
	pcarglive := pcdata[abi.PCDATA_ArgLiveIndex]
	if pcsp != nil {
		patchSymbol(fninfo.Pcln.Pcsp, pcsp)
	}
	if pcfile != nil {
		patchSymbol(fninfo.Pcln.Pcfile, pcfile)
	}
	if pcline != nil {
		patchSymbol(fninfo.Pcln.Pcline, pcline)
	}
	if pcinline != nil {
		patchSymbol(fninfo.Pcln.Pcinline, pcinline)
	}
	if pcstackmap != nil {
		addPCData(f, abi.PCDATA_StackMapIndex, pcstackmap)
	}
	if pcunsafe != nil {
		addPCData(f, abi.PCDATA_UnsafePoint, pcunsafe)
	}
	if pcarglive != nil {
		addPCData(f, abi.PCDATA_ArgLiveIndex, pcarglive)
	} else {
		removePCData(f, abi.PCDATA_ArgLiveIndex)
	}
}

func (c *gcPatchCtxt) PatchFuncData(fsym llvm.TargetSym, fdid int, fdsymbol *llvm.TargetSymbol) {
	f := toLSym(fsym)
	fninfo := f.Func()
	switch fdid {
	case abi.FUNCDATA_StackObjects:
		if fdsymbol == nil {
			fninfo.StackObjects = nil
			removeFuncData(f, fdid)
		} else {
			// prepare data
			for i := range fdsymbol.R {
				r := &fdsymbol.R[i]
				if r.Type != objabi.R_ADDR {
					panic("must be R_ADDR")
				}
				// special handling for the stack object symbol
				r.Type = objabi.R_ADDROFF
			}
			x := base.Ctxt.Lookup(f.Name + ".stkobj")
			if fninfo.StackObjects == nil {
				// new symbol
				fninfo.StackObjects = x
				if x.OnList() {
					panic("newly createwd stkobj cannot be OnList: " + x.Name)
				}
				x.Set(obj.AttrContentAddressable, true)
				patchSymbol(fninfo.StackObjects, fdsymbol)
				objw.Global(x, int32(len(x.P)), obj.RODATA)
				x.Set(obj.AttrStatic, true)
			} else {
				if fninfo.StackObjects != x {
					panic("patch happens after dumpdata")
				}
				if !x.OnList() {
					panic("patch happens after dumpdata")
				}
				patchSymbol(fninfo.StackObjects, fdsymbol)
			}
			addFuncData(f, abi.FUNCDATA_StackObjects, fninfo.StackObjects)
		}
	case abi.FUNCDATA_ArgsPointerMaps:
		if fdsymbol == nil {
			// TODO: we cannot remove the symbol
			// We need to emit an empty map instead.
		} else {
			gcsym := base.Ctxt.GCLocalsSym(fdsymbol.P)
			oringcsym := fninfo.GCArgs
			if gcsym != oringcsym {
				if !gcsym.OnList() {
					objw.Global(gcsym, int32(len(gcsym.P)), obj.RODATA|obj.DUPOK)
				}
				addFuncData(f, abi.FUNCDATA_ArgsPointerMaps, gcsym)
				fninfo.GCArgs = gcsym
			}
		}
	case abi.FUNCDATA_LocalsPointerMaps:
		if fdsymbol == nil {
			// TODO: we cannot remove the symbol
			// We need to emit an empty map instead.
		} else {
			gcsym := base.Ctxt.GCLocalsSym(fdsymbol.P)
			oringcsym := fninfo.GCLocals
			if gcsym != oringcsym {
				if !gcsym.OnList() {
					objw.Global(gcsym, int32(len(gcsym.P)), obj.RODATA|obj.DUPOK)
				}
				addFuncData(f, abi.FUNCDATA_LocalsPointerMaps, gcsym)
				fninfo.GCLocals = gcsym
			}
		}
	}
}

func (c *gcPatchCtxt) SymName(sym llvm.TargetSym) string {
	return toLSym(sym).Name
}

func (c *gcPatchCtxt) SymPkg(sym llvm.TargetSym) string {
	return toLSym(sym).Pkg
}

func (c *gcPatchCtxt) SymSize(sym llvm.TargetSym) int64 {
	return toLSym(sym).Size
}

func (c *gcPatchCtxt) SymAttrNoSplit(sym llvm.TargetSym) bool {
	return toLSym(sym).NoSplit()
}

func (c *gcPatchCtxt) LookupSym(s string) llvm.TargetSym {
	ls := findLSym(s)
	if ls == nil {
		panic(fmt.Errorf("cannot find LSym '%s'@%d, bytes=%v", s, len(s), ([]byte)(s)))
	}
	return toTargetSym(ls)
}

func (c *gcPatchCtxt) FindSym(s string) llvm.TargetSym {
	ls := findLSym(s)
	return toTargetSym(ls)
}

func (c *gcPatchCtxt) LookupTypeSym(tyid string) llvm.TargetSym {
	tysym := types.TypeSymLookup(tyid)
	if tysym == nil {
		tysym = types.TypeSymLookup("noalg." + tyid)
	}
	if tysym == nil {
		panic("cannot find type via id " + tyid)
	}
	return toTargetSym(tysym.Linksym())
}

func (c *gcPatchCtxt) CreateConstDataSymbol(data []byte, size int64, align int32) llvm.TargetSym {
	s := base.Ctxt.ByteArraySym(data, uint64(align))
	return toTargetSym(s)
}

func PatchCtxt(b []byte, verbose int) {
	pctxt := &gcPatchCtxt{}
	llvm.Patch(pctxt, b, verbose)
}

func addFuncData(f *obj.LSym, id int, fd *obj.LSym) {
	funcdata := f.Func().Pcln.Funcdata
	if id >= len(funcdata) {
		newfuncdata := make([]*obj.LSym, id+1)
		copy(newfuncdata, funcdata)
		newfuncdata[id] = fd
		f.Func().Pcln.Funcdata = newfuncdata
	} else {
		if funcdata[id] == nil {
			funcdata[id] = fd
		} else {
			funcdata[id] = fd
		}
	}
}

// remove funcdata with id in f.Pcln.Funcdata
// if Pcln.Funcdata has trailing nil data, shrink the slice
func removeFuncData(f *obj.LSym, id int) {
	funcdata := f.Func().Pcln.Funcdata
	if id >= len(funcdata) {
		// id is not available
		return
	} else {
		i := len(funcdata) - 1
		for ; i >= 0; i-- {
			if i == id {
				funcdata[i] = nil
				break
			}
			if funcdata[i] != nil {
				break
			}
		}
		if i > id {
			funcdata[id] = nil
			// no need to shrink we are done.
			return
		}

		// i must equal id
		f.Func().Pcln.Funcdata = funcdata[:i]
	}
}

func addPCData(f *obj.LSym, id int, gosym *llvm.TargetSymbol) {
	pcdata := f.Func().Pcln.Pcdata
	size := int64(gosym.Size)
	if size != int64(len(gosym.P)) {
		panic(fmt.Errorf("mismatched data size for %s, Size=%d, len(P)=%d", f.Name, size, len(gosym.P)))
	}
	if len(gosym.R) != 0 {
		panic(fmt.Errorf("unknown relocation for %s, Size=%d, len(R)=%d", f.Name, size, len(gosym.R)))
	}
	if id >= len(pcdata) {
		newpcdata := make([]*obj.LSym, id+1)
		copy(newpcdata, pcdata)
		newpcdata[id] = &obj.LSym{
			Type:      objabi.SRODATA,
			Attribute: obj.AttrContentAddressable | obj.AttrPcdata,
			P:         gosym.P,
			Size:      size,
		}
		f.Func().Pcln.Pcdata = newpcdata
	} else {
		if pcdata[id] == nil {
			pcdata[id] = &obj.LSym{
				Type:      objabi.SRODATA,
				Attribute: obj.AttrContentAddressable | obj.AttrPcdata,
				P:         gosym.P,
				Size:      size,
			}
		} else {
			pcdata[id].P = gosym.P
			pcdata[id].Size = size
		}
	}
}

func removePCData(f *obj.LSym, id int) {
	pcdata := f.Func().Pcln.Pcdata
	if id >= len(pcdata) {
		// not available, no need to remove
		return
	} else {
		// we actually "remove" an existing PCData by setting
		// it to empty data.
		if pcdata[id] == nil {
			// new symbol with empty data
			pcdata[id] = &obj.LSym{
				Type:      objabi.SRODATA,
				Attribute: obj.AttrContentAddressable | obj.AttrPcdata,
			}
		} else {
			// clear data in existing symbol
			pcdata[id].P = nil
			pcdata[id].Size = 0
		}
	}
}

func patchSymbol(lsym *obj.LSym, symbol *llvm.TargetSymbol) {
	if lsym == nil {
		panic("cannot patch a nil LSym using " + symbol.HostName)
	}
	lsym.Size = int64(symbol.Size)
	if lsym.Size != int64(len(symbol.P)) {
		panic("Symbol size must be equal to byte length")
	}
	lsym.P = symbol.P
	R := make([]obj.Reloc, 0, len(symbol.R)+10)
	if len(lsym.R) > 0 {
		for _, r := range lsym.R {
			switch r.Type {
			case objabi.R_USEFIELD, objabi.R_USEIFACE, objabi.R_USETYPE, objabi.R_USEIFACEMETHOD, objabi.R_USENAMEDMETHOD:
				R = append(R, r)
			}
		}
	}
	for _, r := range symbol.R {
		R = append(R, convertReloc(r))
	}
	lsym.R = R
}

var linuxLibNames = []string{
	"libc.so.6", "libm.so.6", // hardcoded here
}
var darwinLibNames = []string{
	// TODO: no need, already loaded by runtime/sys_darwin.go??
	"/usr/lib/libSystem.B.dylib", "/usr/lib/libSystem.B.dylib",
}

func lookupCSymbol(s string) *obj.LSym {
	id := s
	var libs []string
	switch buildcfg.GOOS {
	case "darwin":
		id = id[1:] // remove leading underscore
		libs = darwinLibNames
	case "linux":
		libs = linuxLibNames
	default:
		panic("unsupported OS")
	}
	var local string
	var lib string
	switch id {
	case "memset", "memmove", "memcpy":
		local = "libc_" + id
		lib = libs[0]
	case "ceil", "floor", "trunc", "fma":
		local = "libm_" + id
		lib = libs[1]
	default:
		return nil
	}
	ensureCgoPragma(local, id, lib)
	return base.Ctxt.Lookup(local)
}

func ensureCgoPragma(local string, remote string, lib string) {
	for _, prag := range typecheck.Target.CgoPragmas {
		if prag[0] != "cgo_import_dynamic" {
			continue
		}
		if prag[1] != local {
			continue
		}
		if len(prag) <= 2 && local != remote {
			panic("Must have a remote")
		}
		if prag[2] != remote {
			panic("same local but different remote")
		}
		// found and return
		return
	}
	typecheck.Target.CgoPragmas = append(typecheck.Target.CgoPragmas, []string{
		"cgo_import_dynamic", local, remote, lib,
	})
}

func lookupGCSym(ctxt *obj.Link, name string) *obj.LSym {
	var s *obj.LSym
	if strings.HasPrefix(name, "runtime.gcbits.") {
		p := name[15:]
		ptrmask, err := hex.DecodeString(p)
		if err == nil {
			s = ctxt.Lookup(name)
			// Adapted from reflectdata.dgcptrmask; see the source notice above.
			for i, x := range ptrmask {
				s.WriteInt(ctxt, int64(i), 1, int64(x))
			}
			flags := obj.DUPOK | obj.RODATA | obj.LOCAL
			s.Set(obj.AttrLocal, true)
			flags &^= obj.LOCAL
			ctxt.Globl(s, int64(len(ptrmask)), int(flags))
			s.Set(obj.AttrContentAddressable, true)
		}
	}
	return s
}

func makeGoType(llvmstr string) *types.Type {
	var parseType func(str string) *types.Type
	var parseStructType func(str string) *types.Type
	var parseArrayType func(str string) *types.Type
	var splitFields func(str string) []string

	parseType = func(str string) *types.Type {
		if strings.HasPrefix(str, "{") {
			return parseStructType(str)
		} else if strings.HasPrefix(str, "[") {
			return parseArrayType(str)
		} else if strings.HasSuffix(str, "*") {
			return types.NewPtr(parseType(str[:len(str)-1]))
		}
		switch str {
		case "i1":
			return types.Types[types.TBOOL]
		case "i8":
			return types.Types[types.TINT8]
		case "i16":
			return types.Types[types.TINT16]
		case "i32":
			return types.Types[types.TINT32]
		case "i64":
			return types.Types[types.TINT64]
		case "void":
			return types.TypeVoid
		case "float":
			return types.Types[types.TFLOAT32]
		case "double":
			return types.Types[types.TFLOAT64]
		default:
			return nil
		}
	}
	parseStructType = func(str string) *types.Type {
		str = strings.TrimSuffix(strings.TrimPrefix(str, "{"), "}")
		fields := splitFields(str)
		var elems []*types.Field
		for _, field := range fields {
			if field == "" {
				continue
			}
			elems = append(elems, types.NewField(src.NoXPos, nil, parseType(strings.TrimSpace(field))))
		}
		return types.NewStruct(elems)
	}
	parseArrayType = func(str string) *types.Type {
		str = strings.TrimSuffix(strings.TrimPrefix(str, "["), "]")
		lengthstr, elem, found := strings.Cut(str, "x")
		length, _ := strconv.ParseInt(strings.TrimSpace(lengthstr), 10, 64)
		base.Assert(found)
		return types.NewArray(parseType(strings.TrimSpace(elem)), length)
	}
	splitFields = func(str string) []string {
		fields := []string{}
		bracketCount, braceCount, lastSplit := 0, 0, 0
		for i, ch := range str {
			switch ch {
			case '[':
				bracketCount++
			case ']':
				bracketCount--
			case '{':
				braceCount++
			case '}':
				braceCount--
			case ',':
				if bracketCount == 0 && braceCount == 0 {
					fields = append(fields, strings.TrimSpace(str[lastSplit:i]))
					lastSplit = i + 1
				}
			}
		}
		fields = append(fields, strings.TrimSpace(str[lastSplit:]))
		return fields
	}
	return parseType(llvmstr)
}

func findLSym(s string) *obj.LSym {
	if s == "" {
		panic(fmt.Errorf("cannot find LSym '%s'@%d, bytes=%v", s, len(s), ([]byte)(s)))
	}
	ls := base.Ctxt.LookupLLVMGoSymbol(s, lookupGCSym)
	if ls == nil {
		if strings.HasPrefix(s, "_") && !strings.Contains(s, ".") && !strings.Contains(s, ":") {
			s = s[1:] // remove leading '_'
			ls = base.Ctxt.LookupLLVMGoSymbol(s, lookupGCSym)
		}
	}
	// TODO: we cannot call C symbols.
	// These symbols may trigger crashes like "unexpected return PC"
	if ls == nil {
		ls = lookupCSymbol(s)
		if ls != nil {
			panic(fmt.Errorf("We cannot use C symbol: '%s'@%d, bytes=%v", s, len(s), ([]byte)(s)))
		}
	}
	return ls
}
