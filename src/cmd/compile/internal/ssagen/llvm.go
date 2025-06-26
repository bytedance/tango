// Copyright (c) 2018-2022 The TinyGo Authors. All rights reserved.
// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2025-06-26.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Portions of this file are derived from TinyGo v0.25.0's
// compiler/compiler.go and compiler/atomic.go. They include compiler
// configuration and context declarations, debug metadata, LLVM type
// construction, atomic operations, and related comments and helpers.
// These portions and their modifications remain under TinyGo's BSD
// 3-Clause license; see LICENSE-TinyGo in this directory.

//go:build goexperiment.tangollvm && linux && amd64 && cgo && !compiler_bootstrap

package ssagen

import (
	"cmd/compile/internal/abi"
	"cmd/compile/internal/base"
	"cmd/compile/internal/reflectdata"
	"cmd/compile/internal/typecheck"
	"cmd/internal/obj"
	"cmd/internal/objabi"
	"debug/dwarf"
	"fmt"
	"internal/buildcfg"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"cmd/compile/internal/ir"
	"cmd/compile/internal/ssa"
	"cmd/compile/internal/types"
	"cmd/internal/llvm"
	"cmd/internal/obj/x86"
	"cmd/internal/src"
)

// LLVM attribute for flags in textflag.go
// not all flags are needed for functions
const (
	GO_NOSPLIT  = "go-no-split"
	GO_NEEDCTXT = "go-need-ctxt"
	GO_NOFRAME  = "go-no-frame"
	// for internal mangling
	GO_SYM_MORESTACK = "go-sym-morestack"
	// GO_KEEP_ATTR LLVM won't change the value of go-no-split when go-keep-attr is set
	GO_KEEP_ATTR = "go-keep-attr"
)

var (
	LLVMCtxt *compilerContext

	// debugging support
	// denylist and allowlist of function linknames
	// use objdump -t to check function linkname;
	denylist  map[string]struct{}
	allowlist map[string]struct{}
	// allowlist and denylist of packages
	allowedPkgs    map[string]struct{}
	disallowedPkgs map[string]struct{}
)

var (
	BranchLikelyValue   int64
	BranchUnlikelyValue int64
)

const (
	DefaultBranchLikelyValue   = 0
	DefaultBranchUnlikelyValue = 0
)

func init() {
	safeParseInt := func(s string, d int64) int64 {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return d
		}
		return v
	}
	BranchLikelyValue = safeParseInt(os.Getenv("GOLLVMBRANCHLIKELY"), DefaultBranchLikelyValue)
	BranchUnlikelyValue = safeParseInt(os.Getenv("GOLLVMBRANCHUNLIKELY"), DefaultBranchUnlikelyValue)
}

// Currently we support any LLVM bitcode that conforms to the format
// supported by the LLVM backend.
// 1. Stack must be 8 byte aligned in amd64 and no 16 byte align memory operations.
// 2. Every function must be one of the following cases:
//     3.1. Definition with Go internal cconv
//     3.2. LLVM intrinsic (existing or introduced by the LLVM backend)
//     3.3. Declaration with Go internal cconv
//    These conditions means every call instruction can only call Go ABI internal functions
//    or LLVM intrinsic that may be inlined or linked to an existing libc function (e.g., memset).
// 3. Every global variable must be external linkage.
// 4. Every type must be Go compatible.
// 5. Every constant must be Go compatible.

func parseDenylist() {
	// helper function
	stringToMap := func(m map[string]struct{}, s string, sep string) {
		tokens := strings.Split(s, sep)
		for _, t := range tokens {
			m[t] = struct{}{}
		}
	}

	wl := os.Getenv("GOLLVMALLOWLIST")
	if wl != "" {
		allowlist = make(map[string]struct{})
		stringToMap(allowlist, wl, ":")
	}
	wlf := os.Getenv("GOLLVMALLOWLISTFILE")
	if wlf != "" {
		if allowlist == nil {
			allowlist = make(map[string]struct{})
		}
		b, err := os.ReadFile(wlf)
		if err != nil {
			panic(err)
		}
		stringToMap(allowlist, string(b), "\n")
	}
	bl := os.Getenv("GOLLVMDENYLIST")
	if bl != "" {
		denylist = make(map[string]struct{})
		stringToMap(denylist, bl, ":")
	}
	wp := os.Getenv("GOLLVMALLOWEDPKGS")
	if wp != "" {
		allowedPkgs = make(map[string]struct{})
		stringToMap(allowedPkgs, wp, ":")
	}
	wpf := os.Getenv("GOLLVMALLOWEDPKGSFILE")
	if wpf != "" {
		if allowedPkgs == nil {
			allowedPkgs = make(map[string]struct{})
		}
		b, err := os.ReadFile(wpf)
		if err != nil {
			panic(err)
		}
		stringToMap(allowedPkgs, string(b), "\n")
	}
	bp := os.Getenv("GOLLVMDISALLOWEDPKGS")
	if bp != "" {
		disallowedPkgs = make(map[string]struct{})
		stringToMap(disallowedPkgs, bp, ":")
	}
	if disallowedPkgs == nil {
		disallowedPkgs = make(map[string]struct{})
	}

	// Some well known packages that are not worth compiling using LLVM, e.g., too big
	wellKnownDenylist := "github.com/chenzhuoyu/iasm/x86_64:github.com/cloudwego/iasm/x86_64"
	stringToMap(disallowedPkgs, wellKnownDenylist, ":")
	/*
	 * - runtime.stackOverflow is a test function that constructs stackoverflow by calling itself,
	 *   but llvm optimizes it to an unconditional jump via 'tail call elimination(probably)' which
	 *   leads to an infinite loop.
	 */
	wellKnownDenylist = "runtime.stackOverflow"
	if denylist == nil {
		denylist = make(map[string]struct{})
	}
	stringToMap(denylist, wellKnownDenylist, ":")
}

func isDenylisted(s string) bool {
	if len(allowlist) == 0 {
		// if allowlist is empty, we only check denylist
		_, b := denylist[s]
		return b
	} else {
		// if allowlist is not empty, only functions in allowlist are allowed
		_, b := allowlist[s]
		return !b
	}
}

func isPackageAllowed(pkg string) bool {
	if len(allowedPkgs) == 0 {
		return true
	}
	if pkg == "\"\"" {
		pkg = base.Ctxt.Pkgpath
	}
	_, b := allowedPkgs[pkg]
	return b
}

func isPackageDisallowed(pkg string) bool {
	if len(disallowedPkgs) == 0 {
		return false
	}
	if pkg == "\"\"" {
		pkg = base.Ctxt.Pkgpath
	}
	_, b := disallowedPkgs[pkg]
	return b
}

// Explictly call InitLLVM
func InitLLVM() {
	var verbose int
	if i, err := strconv.ParseInt(os.Getenv("GOLLVMVERBOSE"), 10, 32); err == nil {
		verbose = int(i)
	}

	if libname, err := getLibraryName(); err == nil {
		if err := llvm.InitTangoLLVMEnv(libname); err != nil {
			base.Fatalf("llvm: %v\n", err)
			return
		}
	} else {
		base.Fatalf("llvm: %v\n", err)
		return
	}

	// run LLVMInitializeXXX
	llvm.InitializeAll()

	parseDenylist()

	triple, err := llvm.GetLLVMTriple()
	if err != nil {
		panic(err)
	}
	config := &llvm.Config{
		Triple:          triple,
		CPU:             llvm.CPU_X86_64, // see getLLVMTriple below. x86_64 is the only generic CPU we support
		Features:        llvm.GetLLVMFeatures(base.Flag.Cfg.BeastOpt.AMD64TargetFeature),
		CodeModel:       "default",
		RelocationModel: "static",
		SizeLevel:       1,

		// used to control whether to generate DebugLoc in LLVM
		// has nothing to do with optimization
		Debug: true,
	}
	machine, err := llvm.NewTargetMachine(config)
	if err != nil {
		panic(err)
	}
	moduleName := getLLVMModuleName()
	dumpSSA := os.Getenv("GOLLVMDUMPSSA") != ""

	LLVMCtxt = newCompilerContext(moduleName, machine, config, dumpSSA, verbose)

	// TODO: we have to lookup those symbols that may be additionally inserted by LLVM
	// We should remove these lookup once we have unified the rules of LLVM and the gc compiler
	typecheck.LookupRuntimeABI("morestack", obj.ABI0)
	typecheck.LookupRuntimeABI("morestack_noctxt", obj.ABI0)
	typecheck.LookupRuntimeFunc("memclrNoHeapPointers")
	typecheck.LookupRuntimeFunc("memmove")

	typecheck.LookupRuntimeFunc("gcWriteBarrier1")
	typecheck.LookupRuntimeFunc("gcWriteBarrier2")
	typecheck.LookupRuntimeFunc("gcWriteBarrier3")
	typecheck.LookupRuntimeFunc("gcWriteBarrier4")
	typecheck.LookupRuntimeFunc("gcWriteBarrier5")
	typecheck.LookupRuntimeFunc("gcWriteBarrier6")
	typecheck.LookupRuntimeFunc("gcWriteBarrier7")
	typecheck.LookupRuntimeFunc("gcWriteBarrier8")
}

// DebuggerInfo
// TODO: cache results
func (b *builder) getDIFunc(sym *obj.LSym, xpos src.XPos) llvm.Metadata {
	if dif, ok := b.difuncs[sym]; ok {
		return dif
	}
	pos := base.Ctxt.PosTable.Pos(xpos)
	line := int(pos.Line())
	filename := pos.AbsFilename()
	if filename == "" {
		panic("empty filename")
	}
	difile := b.getDIFile(filename)
	diFuncType := b.dibuilder.CreateSubroutineType(llvm.DISubroutineType{
		File: difile,
	})
	dif := b.dibuilder.CreateFunction(difile, llvm.DIFunction{
		Name:         sym.Name,
		File:         difile,
		Line:         line,
		Type:         diFuncType,
		LocalToUnit:  true,
		IsDefinition: true,
		ScopeLine:    0,
		Flags:        llvm.FlagPrototyped,
		Optimized:    true,
	})
	b.difuncs[sym] = dif
	return dif
}

// Convert Go Location (src.XPos) to LLVM Location
func (b *builder) posToDebugLoc(xpos src.XPos) llvm.Metadata {
	if !xpos.IsKnown() {
		panic("oops, must be a known loc")
	}
	llvmloc, ok := b.posMap[xpos]
	if ok {
		return llvmloc
	}
	pos := base.Ctxt.PosTable.Pos(xpos)
	inlidx := pos.Base().InliningIndex()
	if inlidx == -1 {
		// not in an inlined body
		llvmloc = b.dibuilder.CreateDebugLocation(llvm.DebugLoc{Line: pos.Line(), Col: pos.Col(), Scope: b.difunc})
	} else {
		// inlined body
		inlinedcaller := base.Ctxt.InlTree.InlinedFunction(inlidx)
		inlinedcallpos := base.Ctxt.InlTree.CallPos(inlidx)
		inlinedscope := b.getDIFunc(inlinedcaller, xpos) // xpos can give the correct file name.
		llvmloc = b.dibuilder.CreateDebugLocation(llvm.DebugLoc{
			Line:      pos.Line(),
			Col:       pos.Col(),
			Scope:     inlinedscope,
			InlinedAt: b.posToDebugLoc(inlinedcallpos)})
	}
	b.posMap[xpos] = llvmloc
	return llvmloc
}

// Set current location
func (b *builder) setCurLoc(xpos src.XPos) src.XPos {
	old := b.curPos
	b.curPos = xpos
	if b.Debug && xpos.IsKnown() {
		b.SetCurrentDebugLocation2(b.posToDebugLoc(xpos))
	}
	return old
}

// TODO: Unused, copied from tinygo
func (b *builder) getDILocal(variable *ir.Name) llvm.Metadata {
	if dilocal, ok := b.dilocals[variable]; ok {
		// DILocalVariable was already created, return it directly.
		return dilocal
	}

	pos := base.Ctxt.PosTable.Pos(variable.Pos())

	// Check whether this is a function parameter.
	switch variable.Class {
	case ir.PPARAM, ir.PPARAMOUT:
		dilocal := b.dibuilder.CreateParameterVariable(b.difunc, llvm.DIParameterVariable{
			Name:           variable.Name().Sym().Name,
			File:           b.getDIFile(pos.AbsFilename()),
			Line:           int(pos.Line()),
			Type:           b.getDIType(variable.Type()),
			AlwaysPreserve: true,
			ArgNo:          0, // TODO:
		})
		b.dilocals[variable] = dilocal
		return dilocal
	}

	// No, it's not a parameter. Create a regular (auto) variable.
	dilocal := b.dibuilder.CreateAutoVariable(b.difunc, llvm.DIAutoVariable{
		Name:           variable.Name().Sym().Name,
		File:           b.getDIFile(pos.AbsFilename()),
		Line:           int(pos.Line()),
		Type:           b.getDIType(variable.Type()),
		AlwaysPreserve: true,
	})
	b.dilocals[variable] = dilocal
	return dilocal
}

func getLibraryName() (string, error) {
	if buildcfg.GOOS == "linux" {
		return "libLLVM.so", nil
	} else if buildcfg.GOOS == "darwin" {
		return "libLLVM.dylib", nil
	} else {
		return "", fmt.Errorf("unsupported OS: %s", buildcfg.GOOS)
	}
}

// Build the name for the LLVM module.
// The name is only used for debugging.
func getLLVMModuleName() string {
	pkg := types.LocalPkg
	return pkg.Path
}

func (c *compilerContext) writeLLVMModule(str string, mod llvm.Module) {
	// write LLVM Bitcode file for debugging.
	dstfile := base.Flag.LowerO
	ext := path.Ext(dstfile)
	dstfile = dstfile[:len(dstfile)-len(ext)] + str + ".bc"

	var file *os.File
	var err error
	if file, err = os.OpenFile(dstfile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666); err == nil {
		if err = llvm.WriteBitcodeToFile(mod, file); err == nil {
			err = file.Close()
			c.bcfile = dstfile
		}
	}

	if err != nil {
		// We do not panic when we fail to write bitcode file for debugging.
		fmt.Println("WARNING: cannot write bitcode file to", dstfile)
	}
}

func eraseDupOk(m llvm.Module, dupokFunc map[string]bool) llvm.Module {
	elist := make([]llvm.Value, 0)
	for fn := m.FirstFunction(); !fn.IsNil(); fn = llvm.NextFunction(fn) {
		fname := fn.Name()
		if v, ok := dupokFunc[fname]; ok && v {
			elist = append(elist, fn)
		}
	}
	for _, v := range elist {
		v.RemoveFunctionBody()
	}
	return m
}

func (c *compilerContext) summaryOut() {
	if c.Verbose <= 1 {
		return
	}

	su := c.summary
	fmt.Println("[LLVM] Module: ", c.moduleName)
	fmt.Println("[LLVM] Inlined Func Number", su.inlinedFunc)
}

// Compile the LLVM module and emit object file.
func (c *compilerContext) complete() {
	if c.mod.FirstFunction().IsNil() {
		// The module is empty
		return
	}

	defer func() {
		c.ctx.Dispose()
		if r := recover(); r != nil {
			fmt.Println("failed to complete LLVM for module", c.moduleName)
			panic(r)
		}
	}()

	c.saveDebugInfo()

	err := llvm.VerifyModule(c.mod, llvm.PrintMessageAction)
	if err != nil {
		panic(err)
	}

	c.writeLLVMModule("", c.mod)
	s := time.Now()
	var mbytes []byte
	tools, err := llvm.DefaultTools(c.Verbose)
	if err != nil {
		log.Fatal(err)
	}
	llvmCL := os.Getenv("GOLLVMLLCCL")
	llvmOptCL := os.Getenv("GOLLVMOPTCL")
	var args, optArgs []string
	if llvmCL != "" {
		args = strings.Split(llvmCL, " ")
	}
	if llvmOptCL != "" {
		optArgs = strings.Split(llvmOptCL, " ")
	}
	// erase `DupOk` functions in export module.
	erase := func(curr string) {
		if c.moduleName == "runtime" {
			return
		}
		m := c.mod
		m = eraseDupOk(m, c.dupokFunc)
		c.writeLLVMModule("export", m)
	}
	// run opt | llc
	objfile, err := tools.CompileModule(c.bcfile, c.Triple, c.Features, erase, optArgs, args)
	if err != nil {
		log.Fatalln("panic on llvm compile ", c.bcfile, c.Triple)
	}
	mbytes, err = os.ReadFile(objfile)
	if err != nil {
		panic(err)
	}

	emitTime := time.Since(s)
	s = time.Now()
	if c.Verbose >= 1 {
		fmt.Printf("emitting LLVM module %s takes %dms\n", c.moduleName, emitTime.Milliseconds())
	}

	PatchCtxt(mbytes, c.Verbose)
	patchTime := time.Since(s)
	if c.Verbose >= 1 {
		fmt.Printf("patching LLVM module %s takes %dms\n", c.moduleName, patchTime.Milliseconds())
	}
	c.summaryOut()
}

func CompleteLLVM() {
	defer func() {
		LLVMCtxt = nil
	}()
	if LLVMCtxt != nil && !LLVMCtxt.ctx.IsNil() {
		LLVMCtxt.complete()
	}
}

// Config is the configuration for the compiler. Most settings should be copied
// directly from compileopts.Config, it recreated here to decouple the compiler
// package a bit and because it makes caching easier.
//
// This struct can be used for caching: if one of the flags here changes the
// code must be recompiled.

type Summary struct {
	inlinedFunc int
	optedFuncs  int
}

// compilerContext contains function-independent data that should still be
// available while compiling every function. It is not strictly read-only, but
// must not contain function-dependent data such as an IR builder.
type compilerContext struct {
	*llvm.Config
	DumpSSA bool
	Verbose int
	mod     llvm.Module
	ctx     llvm.Context

	// parallel building
	mu sync.Mutex

	// TODO: see comments in getLLVMType
	llvmTypes    map[*types.Type]llvm.Type
	goTypes      map[llvm.Type]*types.Type
	llvmConsts   map[llvm.Value]llvm.Value
	goConsts     map[llvm.Value]*ir.Name
	machine      llvm.TargetMachine
	targetData   llvm.TargetData
	voidType     llvm.Type
	intType      llvm.Type
	int1Type     llvm.Type
	int8Type     llvm.Type
	int16Type    llvm.Type
	int32Type    llvm.Type
	int64Type    llvm.Type
	f32Type      llvm.Type
	f64Type      llvm.Type
	ptrType      llvm.Type // opaque pointer in address space 0
	i8ptrType    llvm.Type
	i8ptrptrType llvm.Type
	i64ptrType   llvm.Type
	uintptrType  llvm.Type

	// special fake interface receiver
	fakeRecvType llvm.Type
	// special Go runtime method
	// See ir.Syms.GCWriteBarrier
	gcWriteBarrierFn     [8]llvm.Value
	gcWriteBarrierFnType llvm.Type

	// mapping Go types to low-level struct types
	synthString *types.Type
	synthEface  *types.Type
	synthSlice  *types.Type
	synthIface  *types.Type
	synthFunc   *types.Type
	synthChan   *types.Type

	// intrinsics
	goPreAllocatedID int

	// the map for the declared type for runtime builtins
	runtimeBuiltdinMap map[string]llvm.Type

	// cache
	linkname2Node map[string]ir.Node

	// DebugInfo
	dibuilder       *llvm.DIBuilder
	cu              llvm.Metadata
	difiles         map[string]llvm.Metadata
	difuncs         map[*obj.LSym]llvm.Metadata
	ditypes         map[*types.Type]llvm.Metadata
	anonymousStruct int
	moduleName      string

	summary   *Summary
	bcfile    string
	dupokFunc map[string]bool
}

// TODO: currently we are not building package runtime.
// Use the given LSym even we know we are calling runtime.gcWriteBarrier
func (c *compilerContext) getGCWriteBarrierFn(n int64) llvm.Value {
	fn := c.gcWriteBarrierFn[n-1]
	if !fn.IsNil() {
		return fn
	}

	s := ir.Syms.GCWriteBarrier[n-1]
	name := c.getGoFunctionLLVMName(s)
	if !c.mod.NamedFunction(name).IsNil() {
		// We cannot access runtime.gcWriteBarrierN from source code
		base.Fatalf("cannot call %s directly", s.Name)
		return llvm.Value{}
	}

	fn = llvm.AddFunction(c.mod, name, c.gcWriteBarrierFnType)
	fn.SetLinkage(llvm.ExternalLinkage)
	c.gcWriteBarrierFn[n-1] = fn
	fn.SetFunctionCallConv(llvm.GO_ABIINTERNAL_CCONV)
	return fn
}

func (c *compilerContext) saveDebugInfo() {
	if !c.Debug {
		return
	}
	c.mod.AddNamedMetadataOperand("llvm.module.flags",
		c.ctx.MDNode([]llvm.Metadata{
			llvm.ConstInt(c.ctx.Int32Type(), 2, false).ConstantAsMetadata(), // Warning on mismatch
			c.ctx.MDString("Debug Info Version"),
			llvm.ConstInt(c.ctx.Int32Type(), 3, false).ConstantAsMetadata(), // DWARF version
		}),
	)
	c.mod.AddNamedMetadataOperand("llvm.module.flags",
		c.ctx.MDNode([]llvm.Metadata{
			llvm.ConstInt(c.ctx.Int32Type(), 7, false).ConstantAsMetadata(), // Max on mismatch
			c.ctx.MDString("Dwarf Version"),
			llvm.ConstInt(c.ctx.Int32Type(), 4, false).ConstantAsMetadata(),
		}),
	)
	c.saveLocalFileTable()
	c.dibuilder.Finalize()
	c.dibuilder.Destroy()
}

func (c *compilerContext) saveLocalFileTable() {
	strtab := make([]string, 0, len(c.difiles))
	for k := range c.difiles {
		strtab = append(strtab, k)
	}
	sort.Slice(strtab, func(x, y int) bool {
		return strtab[x] < strtab[y]
	})
	mdarray := make([]llvm.Metadata, 0, len(strtab)<<1)
	for _, s := range strtab {
		if s == "" {
			panic("empty file")
		}
		dir, b := spiltFilename(s)
		mdarray = append(mdarray, c.ctx.MDString(dir), c.ctx.MDString(b))
		// src.NewFileBase in pos.go
		fileindex := base.Ctxt.PosTable.FileIndex(s)
		if fileindex == -1 {
			panic(fmt.Errorf("cannot find file %s", s))
		}
		mdarray = append(mdarray, llvm.ConstInt(c.int32Type, uint64(fileindex), false).ConstantAsMetadata())
	}
	c.mod.AddNamedMetadataOperand("go.local.filetab", c.ctx.MDNode(mdarray))
}

// Get the DWARF type for this Go type.
func (c *compilerContext) getDIType(typ *types.Type) llvm.Metadata {
	if md, ok := c.ditypes[typ]; ok {
		return md
	}
	md := c.createDIType(typ)
	c.ditypes[typ] = md
	return md
}

func (c *compilerContext) createBasicDIType(typ *types.Type, sizeInBytes uint64, encoding llvm.DwarfTypeEncoding) llvm.Metadata {
	return c.dibuilder.CreateBasicType(llvm.DIBasicType{
		Name:       typ.String(),
		SizeInBits: sizeInBytes * 8,
		Encoding:   encoding,
	})
}

// createDIType creates a new DWARF type. Don't call this function directly,
// call getDIType instead.
func (c *compilerContext) createDIType(typ *types.Type) llvm.Metadata {
	// typ = typeParamUnderlyingType(typ)
	llvmType := c.getLLVMType(typ)
	sizeInBytes := c.targetData.TypeAllocSize(llvmType)
	switch typ.Kind() {
	case types.TARRAY:
		return c.dibuilder.CreateArrayType(llvm.DIArrayType{
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			ElementType: c.getDIType(typ.Elem()),
			Subscripts: []llvm.DISubrange{
				{
					Lo:    0,
					Count: typ.NumElem(),
				},
			},
		})
	case types.TBOOL:
		return c.createBasicDIType(typ, sizeInBytes, llvm.DW_ATE_boolean)
	case types.TINT8, types.TINT16, types.TINT32, types.TINT64, types.TINT:
		return c.createBasicDIType(typ, sizeInBytes, llvm.DW_ATE_signed)
	case types.TUINT8, types.TUINT16, types.TUINT32, types.TUINT64, types.TUINT, types.TUINTPTR:
		return c.createBasicDIType(typ, sizeInBytes, llvm.DW_ATE_unsigned)
	case types.TFLOAT32, types.TFLOAT64:
		return c.createBasicDIType(typ, sizeInBytes, llvm.DW_ATE_float)
	case types.TCOMPLEX64, types.TCOMPLEX128:
		return c.createBasicDIType(typ, sizeInBytes, llvm.DW_ATE_complex_float)
	case types.TUNSAFEPTR:
		return c.dibuilder.CreatePointerType(llvm.DIPointerType{
			Name:         "unsafe.Pointer",
			SizeInBits:   c.targetData.TypeAllocSize(llvmType) * 8,
			AlignInBits:  uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			AddressSpace: 0,
		})
	case types.TSTRING:
		return c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
			Name:        "string",
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			Elements: []llvm.Metadata{
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "ptr",
					SizeInBits:   c.targetData.TypeAllocSize(c.i8ptrType) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.i8ptrType)) * 8,
					OffsetInBits: 0,
					Type:         c.getDIType(types.NewPtr(types.Types[types.TUINT8])),
				}),
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "len",
					SizeInBits:   c.targetData.TypeAllocSize(c.uintptrType) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.uintptrType)) * 8,
					OffsetInBits: c.targetData.ElementOffset(llvmType, 1) * 8,
					Type:         c.getDIType(types.Types[types.TINT]),
				}),
			},
		})
	case types.TCHAN:
		panic("TODO")
	case types.TINTER:
		return c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
			Name:        "interface",
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			Elements: []llvm.Metadata{
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "typ",
					SizeInBits:   c.targetData.TypeAllocSize(c.i8ptrType) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.i8ptrType)) * 8,
					OffsetInBits: 0,
					Type:         c.getDIType(types.NewPtr(types.Types[types.TUINT8])),
				}),
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "data",
					SizeInBits:   c.targetData.TypeAllocSize(c.i8ptrType) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.i8ptrType)) * 8,
					OffsetInBits: c.targetData.ElementOffset(llvmType, 1) * 8,
					Type:         c.getDIType(types.NewPtr(types.Types[types.TUINT8])),
				}),
			},
		})
	case types.TMAP:
		return c.getDIType(reflectdata.MapType())
	case types.TFORW:
		return c.dibuilder.CreateTypedef(llvm.DITypedef{
			Type: c.getDIType(typ.Underlying()),
			Name: typ.String(),
		})
	case types.TPTR:
		return c.dibuilder.CreatePointerType(llvm.DIPointerType{
			Pointee:      c.getDIType(typ.Elem()),
			SizeInBits:   c.targetData.TypeAllocSize(llvmType) * 8,
			AlignInBits:  uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			AddressSpace: 0,
		})
	case types.TFUNC:
		// TODO: should be a FUNC, e.g., see type funcval in runtime/runtime2.go
		// actually a closure
		fields := llvmType.StructElementTypes()
		return c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			Elements: []llvm.Metadata{
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "fn",
					SizeInBits:   c.targetData.TypeAllocSize(fields[0]) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(fields[0])) * 8,
					OffsetInBits: c.targetData.ElementOffset(llvmType, 0) * 8,
					Type:         c.getDIType(types.Types[types.TUINTPTR]),
				}),
			},
		})
	case types.TSLICE:
		fields := llvmType.StructElementTypes()
		return c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
			Name:        typ.String(),
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			Elements: []llvm.Metadata{
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "ptr",
					SizeInBits:   c.targetData.TypeAllocSize(fields[0]) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(fields[0])) * 8,
					OffsetInBits: 0,
					Type:         c.getDIType(types.NewPtr(typ.Elem())),
				}),
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "len",
					SizeInBits:   c.targetData.TypeAllocSize(c.uintptrType) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.uintptrType)) * 8,
					OffsetInBits: c.targetData.ElementOffset(llvmType, 1) * 8,
					Type:         c.getDIType(types.Types[types.TINT]),
				}),
				c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
					Name:         "cap",
					SizeInBits:   c.targetData.TypeAllocSize(c.uintptrType) * 8,
					AlignInBits:  uint32(c.targetData.ABITypeAlignment(c.uintptrType)) * 8,
					OffsetInBits: c.targetData.ElementOffset(llvmType, 2) * 8,
					Type:         c.getDIType(types.Types[types.TINT]),
				}),
			},
		})
	case types.TSTRUCT:
		// Placeholder metadata node, to be replaced afterwards.
		temporaryMDNode := c.dibuilder.CreateReplaceableCompositeType(llvm.Metadata{}, llvm.DIReplaceableCompositeType{
			Tag:         dwarf.TagStructType,
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
		})
		c.ditypes[typ] = temporaryMDNode
		elements := make([]llvm.Metadata, typ.NumFields())
		for i := range elements {
			fieldType := typ.FieldType(i)
			llvmField := c.getLLVMType(fieldType)
			elements[i] = c.dibuilder.CreateMemberType(llvm.Metadata{}, llvm.DIMemberType{
				Name:         typ.FieldName(i),
				SizeInBits:   c.targetData.TypeAllocSize(llvmField) * 8,
				AlignInBits:  uint32(c.targetData.ABITypeAlignment(llvmField)) * 8,
				OffsetInBits: c.targetData.ElementOffset(llvmType, i) * 8,
				Type:         c.getDIType(fieldType),
			})
		}
		md := c.dibuilder.CreateStructType(llvm.Metadata{}, llvm.DIStructType{
			SizeInBits:  sizeInBytes * 8,
			AlignInBits: uint32(c.targetData.ABITypeAlignment(llvmType)) * 8,
			Elements:    elements,
		})
		temporaryMDNode.ReplaceAllUsesWith(md)
		return md
	default:
		panic("unknown type while generating DWARF debug type: " + typ.String())
	}
}

func newCompilerContext(moduleName string, machine llvm.TargetMachine, config *llvm.Config, dumpSSA bool, verbose int) *compilerContext {
	c := &compilerContext{
		Config:          config,
		DumpSSA:         dumpSSA,
		Verbose:         verbose,
		machine:         machine,
		llvmTypes:       make(map[*types.Type]llvm.Type),
		goTypes:         make(map[llvm.Type]*types.Type),
		llvmConsts:      make(map[llvm.Value]llvm.Value),
		goConsts:        make(map[llvm.Value]*ir.Name),
		targetData:      machine.CreateTargetData(),
		moduleName:      moduleName,
		linkname2Node:   make(map[string]ir.Node),
		summary:         &Summary{},
		dupokFunc:       make(map[string]bool),
		anonymousStruct: 0,
	}

	c.ctx = llvm.NewContext()
	c.mod = c.ctx.NewModule(moduleName)
	c.mod.SetTarget(config.Triple)
	dataLayoutString := c.targetData.String()
	c.mod.SetDataLayout(dataLayoutString)
	c.mod.AddNamedMetadataOperand("go.pkgpath", c.ctx.MDNode([]llvm.Metadata{
		c.ctx.MDString(base.Ctxt.Pkgpath),
	}))
	c.mod.AddNamedMetadataOperand("go.version", c.ctx.MDNode([]llvm.Metadata{
		c.ctx.MDString(buildcfg.Version),
	}))
	c.mod.AddNamedMetadataOperand("llvm.module.flags",
		c.ctx.MDNode([]llvm.Metadata{
			llvm.ConstInt(c.ctx.Int32Type(), 4, false).ConstantAsMetadata(), // Max on mismatch
			c.ctx.MDString("override-stack-alignment"),
			llvm.ConstInt(c.ctx.Int32Type(), 8, false).ConstantAsMetadata(),
		}),
	)

	// init numeric types
	c.voidType = c.ctx.VoidType()
	c.int1Type = c.ctx.Int1Type()
	c.int8Type = c.ctx.Int8Type()
	c.int16Type = c.ctx.Int16Type()
	c.int32Type = c.ctx.Int32Type()
	c.int64Type = c.ctx.Int64Type()
	c.f32Type = c.ctx.FloatType()
	c.f64Type = c.ctx.DoubleType()
	if c.targetData.PointerSize() == 8 {
		// 64 bits target
		c.intType = c.ctx.Int64Type()
		c.uintptrType = c.intType
	} else {
		panic(fmt.Errorf("unsupported pointer size: %d", c.targetData.PointerSize()))
	}

	c.ptrType = c.ctx.PointerType(0)
	c.gcWriteBarrierFnType = llvm.FunctionType(c.ptrType, []llvm.Type{}, false)
	c.i8ptrType = llvm.PointerType(c.ctx.Int8Type(), 0)
	c.i8ptrptrType = llvm.PointerType(c.i8ptrType, 0)
	c.i64ptrType = llvm.PointerType(c.int64Type, 0)
	c.initBuiltinTypeMapping()
	c.fakeRecvType = c.getLLVMType(types.FakeRecvType())

	c.goPreAllocatedID = llvm.LookupIntrinsicID("llvm.go.preallocated")
	if c.goPreAllocatedID == 0 {
		panic("cannot find intrinsic id")
	}

	if c.Debug {
		c.dibuilder = llvm.NewDIBuilder(c.mod)
		// A compile unit here may include all go files in the package.
		c.cu = c.dibuilder.CreateCompileUnit(llvm.DICompileUnit{
			Language:  llvm.DW_LANG_Go, // DW_LANG_Go
			File:      moduleName,      // Use unknown because we put all files of the same package into a single IR module
			Dir:       "",
			Producer:  "TangoLLVM",
			Optimized: true,
		})
		c.difiles = make(map[string]llvm.Metadata)
		c.ditypes = make(map[*types.Type]llvm.Metadata)
		c.difuncs = make(map[*obj.LSym]llvm.Metadata)
	}
	return c
}

// TODO: init TSLICE, TINTERFACE, TSTRING, TCOMPLEX here
// Synthesizes a Go struct type to represent the Go high-level type,
// e.g., string, interface{}
func (c *compilerContext) initBuiltinTypeMapping() {
	fname := types.BuiltinPkg.Lookup
	nxp := src.NoXPos
	bp := types.NewPtr(types.Types[types.TUINT8])
	it := types.Types[types.TINT]
	unsp := types.Types[types.TUNSAFEPTR]
	c.synthString = types.NewStruct([]*types.Field{
		types.NewField(nxp, fname("data"), bp),
		types.NewField(nxp, fname("len"), it),
	})
	c.synthEface = types.NewStruct([]*types.Field{
		types.NewField(nxp, fname("_type"), unsp),
		types.NewField(nxp, fname("data"), unsp),
	})
	c.synthSlice = types.NewStruct([]*types.Field{
		types.NewField(nxp, fname("array"), bp),
		types.NewField(nxp, fname("len"), it),
		types.NewField(nxp, fname("cap"), it),
	})
	c.synthIface = types.NewStruct([]*types.Field{
		types.NewField(nxp, fname("tab"), unsp),
		types.NewField(nxp, fname("data"), unsp),
	})
	fn := types.Types[types.TUINTPTR]
	c.synthFunc = types.NewPtr(types.NewStruct([]*types.Field{
		types.NewField(nxp, fname("fn"), fn),
	}))
	c.synthChan = types.NewPtr(types.NewStruct([]*types.Field{}))
	c.runtimeBuiltdinMap = make(map[string]llvm.Type)
}

// builder contains all information relevant to build a single function.
type builder struct {
	*compilerContext
	llvm.Builder
	s          *state
	f          *ssa.Func
	llvmFn     llvm.Value
	llvmFnType llvm.Type

	locals map[*ir.Name]llvm.Value // local variables
	values map[*ssa.Value]llvm.Value
	blocks map[*ssa.Block]llvm.BasicBlock

	// use Pos of the function type first and switch to Pos for each block/instruction
	// when processing the block/instruction
	curPos src.XPos

	// for logging
	functag string

	reachable []bool

	difunc   llvm.Metadata
	dilocals map[*ir.Name]llvm.Metadata
	// map each xpos to LLVM DebugLoc
	posMap map[src.XPos]llvm.Metadata

	memOrderCache map[[2]*ssa.Value]int
	inlinable     bool
}

// getDIFile returns a DIFile metadata node for the given filename. It tries to
// use one that was already created, otherwise it falls back to creating a new
// one.
func (c *compilerContext) getDIFile(filename string) llvm.Metadata {
	if filename == "" {
		panic("empty filename")
	}
	if _, ok := c.difiles[filename]; !ok {
		dir, file := spiltFilename(filename)
		c.difiles[filename] = c.dibuilder.CreateFile(file, dir)
	}
	return c.difiles[filename]
}

func spiltFilename(filename string) (string, string) {
	dir, file := filepath.Split(filename)
	if dir != "" {
		dir = dir[:len(dir)-1]
	}
	return dir, file
}

func newLLVMBuilder(s *state) *builder {
	if base.Flag.Cfg.BeastOpt.LLVM == 0 {
		return nil
	}

	if LLVMCtxt == nil {
		return nil
	}

	return &builder{
		compilerContext: LLVMCtxt,
		s:               s,
		f:               s.f,
		locals:          make(map[*ir.Name]llvm.Value),
		values:          make(map[*ssa.Value]llvm.Value),
		blocks:          make(map[*ssa.Block]llvm.BasicBlock),
		curPos:          s.curfn.Pos(),
		functag:         computeLogTag(s.curfn),
		inlinable:       s.f.Frontend().(*ssafn).curfn.InlinabilityChecked(),
	}
}

func computeLogTag(fn *ir.Func) string {
	name := fn.LSym.Name
	if strings.HasPrefix(name, "\"\".") {
		name = base.Ctxt.Pkgpath + name[2:]
	}
	return name
}

func (c *compilerContext) getLLVMType(goType *types.Type) llvm.Type {
	llvmType, ok := c.llvmTypes[goType]
	if ok {
		return llvmType
	}

	// Not already created, so adding this type to the cache.
	llvmType = c.makeLLVMType(goType)
	c.llvmTypes[goType] = llvmType
	return llvmType
}

// makeLLVMType creates a LLVM type for a Go type. Don't call this, use
// getLLVMType instead.
func (c *compilerContext) makeLLVMType(typ *types.Type) llvm.Type {
	switch typ.Kind() {
	case types.TARRAY:
		elemType := c.getLLVMType(typ.Elem())
		return llvm.ArrayType(elemType, int(typ.NumElem()))
	case types.TBOOL:
		return c.int1Type
	case types.TINT8, types.TUINT8:
		return c.int8Type
	case types.TINT16, types.TUINT16:
		return c.int16Type
	case types.TINT32, types.TUINT32:
		return c.int32Type
	case types.TINT64, types.TUINT64:
		return c.int64Type
	case types.TINT, types.TUINT:
		return c.intType
	case types.TUINTPTR:
		return c.uintptrType
	case types.TUNSAFEPTR:
		return c.i8ptrType
	case types.TFLOAT32:
		return c.f32Type
	case types.TFLOAT64:
		return c.f64Type
	case types.TCOMPLEX64:
		return c.ctx.StructType([]llvm.Type{c.f32Type, c.f32Type}, false)
	case types.TCOMPLEX128:
		return c.ctx.StructType([]llvm.Type{c.f64Type, c.f64Type}, false)
	case types.TSTRING:
		return c.getLLVMType(c.synthString)
	case types.TCHAN:
		return c.getLLVMType(c.synthChan)
	case types.TINTER:
		return c.getLLVMType(c.synthIface)
	case types.TMAP:
		return c.i8ptrType // llvm.PointerType(c.getLLVMType(types.NewPtr(typ.MapType().Hmap)), 0)
	case types.TFORW:
		base.Fatalf("struct type should be resolved already for resursive types, got %v", typ)
		return llvm.Type{}
	case types.TPTR:
		// avoid recursive type constructing
		ele := typ.Elem()
		if ele.Kind() == types.TPTR || ele.Kind() == types.TARRAY {
			// only TPTR, TARRAY and TSTRUCT can cause trouble and need heavy check.
			// TSTRUCT is avoided by using named struct.
			// Here we only need to handle TPTR and TARRAY.
			//
			// pre-allocated space
			var space [8]*types.Type
			// rare case: use a slice instead of map to track visited
			visited := append(space[:0], typ)
			// We could reuse t.Recur to mark visited types,
			// but it requires to be reset at last.
			hasVisited := func(t *types.Type) bool {
				for _, v := range visited {
					if v == t {
						return true
					}
				}
				return false
			}
			for {
				if ele == typ || hasVisited(ele) {
					return llvm.PointerType(c.ctx.StructType([]llvm.Type{}, false), 0)
				}
				visited = append(visited, ele)
				if ele.Kind() == types.TPTR || ele.Kind() == types.TARRAY {
					ele = ele.Elem()
				} else {
					break
				}
			}
		}
		// use typ.Elem() since ele may be updated
		ptrTo := c.getLLVMType(typ.Elem())
		return llvm.PointerType(ptrTo, 0)
	case types.TFUNC:
		// see struct funcval in src/runtime/runtime2.go
		return c.getLLVMType(c.synthFunc)
	case types.TSLICE:
		return c.getLLVMType(c.synthSlice)
	case types.TSTRUCT:
		var ret llvm.Type
		if typ.Sym() != nil {
			typename := typ.LinkString()
			ret = c.ctx.GetTypeByName(typename)
			if !ret.IsNil() {
				return ret
			}
			ret = c.ctx.StructCreateNamed(typename)
			c.llvmTypes[typ] = ret
		} else {
			var anoName string
			c.anonymousStruct++
			if typ == c.synthIface {
				anoName = fmt.Sprintf("interface$%d", c.anonymousStruct)
			} else if typ == c.synthString {
				anoName = fmt.Sprintf("string$%d", c.anonymousStruct)
			} else if typ == c.synthSlice {
				anoName = fmt.Sprintf("slice$%d", c.anonymousStruct)
			} else {
				anoName = fmt.Sprintf("struct$%d", c.anonymousStruct)
			}
			ret = c.ctx.StructCreateNamed(anoName)
			c.llvmTypes[typ] = ret
		}

		members := make([]llvm.Type, typ.NumFields())
		for i := 0; i < typ.NumFields(); i++ {
			members[i] = c.getLLVMType(typ.FieldType(i))
		}
		ret.StructSetBody(members, false)
		return ret
	case types.TTUPLE:
		// a pair
		first := typ.FieldType(0)
		second := typ.FieldType(1)
		if first.IsMemory() {
			// see the implementation of IsMemory
			// we only check the second for TTUPLE
			panic("TODO: we need document of Go SSA")
		}
		if second.IsMemory() {
			return c.getLLVMType(first)
		}
		return c.ctx.StructType([]llvm.Type{
			c.getLLVMType(first), c.getLLVMType(second),
		}, false)
	case types.TRESULTS:
		if typ.NumFields() == 1 {
			return c.ctx.VoidType()
		}
		if typ.NumFields() == 2 {
			return c.getLLVMType(typ.FieldType(0))
		}
		// see comments in types/type.go
		members := make([]llvm.Type, typ.NumFields()-1)
		for i := 0; i < typ.NumFields()-1; i++ {
			members[i] = c.getLLVMType(typ.FieldType(i))
		}
		return c.ctx.StructType(members, false)
	case types.TSSA:
		if typ.IsVoid() {
			return c.ctx.VoidType()
		}
		// TypeInt128.
		if typ == types.TypeInt128 {
			return c.ctx.StructType([]llvm.Type{c.int64Type, c.int64Type}, false)
		}
		shouldNotReachHere()
		return llvm.Type{}
	default:
		panic("unknown type: " + typ.String())
	}
}

func (b *builder) createFunctionStart() {
	f := b.f
	curpos := f.Entry.Pos
	b.llvmFn, b.llvmFnType = b.getFunctionByAuxCallABI(f.OwnAux)

	if b.Debug {
		b.difunc = b.getDIFunc(f.OwnAux.Fn, b.curfn().Nname.Pos())
		b.llvmFn.SetSubprogram(b.difunc)
		b.posMap = make(map[src.XPos]llvm.Metadata)
	}

	b.setCurLoc(curpos)
	if !b.llvmFn.IsDeclaration() {
		base.FatalfAt(b.f.Type.Pos(), "%s redeclared in this program", b.llvmFn.Name())
		return
	}

	curfn := b.curfn()
	if curfn.LSym.NoFrame() {
		b.llvmFn.AddFunctionAttr(b.ctx.CreateStringAttribute(GO_NOFRAME, "true"))
	}
	if curfn.LSym.NoSplit() {
		b.llvmFn.AddFunctionAttr(b.ctx.CreateStringAttribute(GO_NOSPLIT, "true"))
	}
	if curfn.LSym.NeedCtxt() {
		b.llvmFn.AddFunctionAttr(b.ctx.CreateStringAttribute(GO_NEEDCTXT, "true"))
	}
	pkg := curfn.LSym.Pkg
	if pkg == "runtime" || pkg == "runtime_test" {
		b.llvmFn.AddFunctionAttr(b.ctx.CreateStringAttribute(GO_KEEP_ATTR, "true"))
	}

	// we need mangling for none-goobj format
	if !curfn.LSym.NoSplit() {
		// we may split the stack
		var symbol = "runtime.morestack"
		if !curfn.LSym.NeedCtxt() {
			symbol = "runtime.morestack_noctxt"
		}
		// see splitstack in obj6.go
		// morestack is abi0
		symbol = b.getFunctionLLVMName2(symbol, obj.ABI0)
		b.llvmFn.AddFunctionAttr(b.ctx.CreateStringAttribute(GO_SYM_MORESTACK, symbol))
	}

	// set gc strategy
	b.llvmFn.SetGC("go")
}

func (b *builder) createBlock(block *ssa.Block, llvmBlock llvm.BasicBlock) {
	oldpos := b.setCurLoc(block.Pos)
	defer b.setCurLoc(oldpos)
	if b.DumpSSA {
		fmt.Fprintf(os.Stderr, "%s: %s\n", b.functag, llvmBlock.Name())
	}
	b.SetInsertPointAtEnd(llvmBlock)
	created := make(map[*ssa.Value]bool)
	n := 0

	// topological sort
	indegree := make(map[*ssa.Value]int)
	uses := make(map[*ssa.Value][]*ssa.Value)
	memdefCount := make(map[*ssa.Value]int)
	// compute indegree for all Values
	for _, v := range block.Values {
		for _, w := range v.Args {
			if w != nil && w.Block == block {
				indegree[v]++
				uses[w] = append(uses[w], v)
			}
		}
		if u := b.memUse(v); u != nil {
			memdefCount[u]++
		}
		if v.Type.IsMemory() {
			// v must NOT be a def that is Phi, InitMem or depends on some value in previous blocks
			memdefCount[v]++
		}
	}

	// create phi nodes first
	for _, v := range block.Values {
		if v.Op != ssa.OpPhi {
			continue
		}
		if v.Type.IsMemory() {
			// v must NOT be a def that is Phi, InitMem or depends on some value in previous blocks
			memdefCount[v]--
		}
		b.createInstruction(v)
		created[v] = true
		n++
		for _, u := range uses[v] {
			indegree[u]--
		}
	}

	// go over Values in original order, and create the corresponding LLVM
	// instruction whenever its indegree is zero; update the indegree of the uses
	// along the way
	for n < len(block.Values) {
		for _, v := range block.Values {
			if _, ok := created[v]; ok {
				continue
			}
			if indegree[v] > 0 {
				continue
			}
			u, d := b.memUse(v), b.memDef(v)
			if u != nil {
				if d != nil && memdefCount[u] > 1 {
					// a use with def, must wait all sibling usage are done.
					// otherwise the def will override its use.
					continue
				}
				// a use without def, simply decrease usecount
				memdefCount[u]--
			}
			if d != nil {
				memdefCount[d]--
			}
			b.createInstruction(v)
			created[v] = true
			n++
			for _, u := range uses[v] {
				indegree[u]--
			}
			break // important: this makes topo-sort stable
		}
	}

	switch block.Kind {
	case ssa.BlockFirst, ssa.BlockPlain:
		succ := block.Succs[0].Block()
		succLLVM := b.blocks[succ]
		b.CreateBr(succLLVM)
	case ssa.BlockIf:
		if block.NumControls() != 1 {
			base.FatalfAt(block.Pos, "BlockIf should have only one control")
		}
		if len(block.Succs) != 2 {
			base.FatalfAt(block.Pos, "BlockIf should have two successors")
		}
		c := b.getValue(block.ControlValues()[0])
		succ0 := b.blocks[block.Succs[0].Block()]
		succ1 := b.blocks[block.Succs[1].Block()]
		br := b.CreateCondBr(c, succ0, succ1)
		if block.Likely != ssa.BranchUnknown {
			mdid := b.ctx.MDKindID("prof")
			mdlikely := llvm.ConstInt(b.int32Type, uint64(BranchLikelyValue), false).ConstantAsMetadata()
			mdunlikely := llvm.ConstInt(b.int32Type, uint64(BranchUnlikelyValue), false).ConstantAsMetadata()
			mdtype := b.ctx.MDString("branch_weights")
			mdexpected := b.ctx.MDString("expected")
			var mdarray []llvm.Metadata
			if block.Likely == ssa.BranchLikely {
				mdarray = []llvm.Metadata{mdtype, mdexpected, mdlikely, mdunlikely}
			} else {
				mdarray = []llvm.Metadata{mdtype, mdexpected, mdunlikely, mdlikely}
			}
			br.SetMetadata(mdid, b.ctx.MDNode(mdarray))
		}
	case ssa.BlockDefer:
		// TODO: reimplement Defer
		call := block.ControlValues()[0]
		if block.ControlValues()[0].Op != ssa.OpStaticCall {
			base.FatalfAt(block.Pos, "BlockDefer should be staticcall")
		}
		c := b.getValue(call)
		// b.getValue(block.ControlValues()[0])
		// rax := b.ctx.MDNode([]llvm.Metadata{b.ctx.MDString("rax")})
		// target := b.createIntrinsicCallWithArgs("llvm.read_register.i64", []llvm.Value{llvm.MetadataAsValue(b.ctx, rax)}, b.int64Type)
		// c := b.CreateCall(target, []llvm.Value{}, "")
		succ0 := b.blocks[block.Succs[0].Block()]
		succ1 := b.blocks[block.Succs[1].Block()]
		b.CreateCondBr(b.integerConvert(c, b.int1Type, false), succ1, succ0)
	case ssa.BlockRet:
		ret := b.getValue(block.ControlValues()[0])
		b.createReturnByValue(block, ret)
	case ssa.BlockRetJmp:
		// gc doesn't do optimization for tailcall.
		control := block.ControlValues()[0]
		if control.Op == ssa.OpSelectN {
			control = control.Args[0]
		}
		if control.Op != ssa.OpTailCall {
			base.FatalfAt(block.Pos, "must be tail call, got %s", control.Op.String())
		}
		// ignore the last mem arg
		retType := b.llvmFnType.ReturnType()
		switch retType.TypeKind() {
		case llvm.VoidTypeKind:
			b.CreateRetVoid()
		default:
			retVal := b.getValue(control)
			retVal = b.patchValueType(retVal, retType)
			if retVal.Type() != retType {
				base.FatalfAt(block.Pos, "cannot cast %s to %s", retVal.Type().String(), retType.String())
			}
			b.CreateRet(retVal)
		}

	case ssa.BlockExit:
		control := block.ControlValues()[0]
		if control.Op == ssa.OpSelectN {
			control = control.Args[0]
		}
		if control.Op == ssa.OpStaticCall {
			call := b.getValue(control)
			callee := call.CalledValue()
			fn := callee.IsAFunction()
			if fn.IsNil() {
				base.FatalfAt(block.Pos, "the callee of a static call must be a function, got %v", callee.Type())
			}

			// set a method that can panic as noreturn
			kind := llvm.AttributeKindID("noreturn")
			if fn.GetEnumFunctionAttribute(kind).IsNil() {
				fn.AddFunctionAttr(b.ctx.CreateEnumAttribute(kind, 0))
			}
		}
		b.CreateUnreachable()
	case ssa.BlockJumpTable:
		// The default case has been selected before the control flow enters the BlockJumpTable.
		// We create a fake unreachable block as the default target to make LLVM valid.
		exit := b.ctx.AddBasicBlock(b.llvmFn, block.String()+".switchexit")
		b.SetInsertPointAtEnd(exit)
		b.CreateUnreachable()
		b.SetInsertPointAtEnd(llvmBlock)
		idx := block.ControlValues()[0]
		idxVal := b.getValue(idx)
		switchBr := b.CreateSwitch(idxVal, exit, len(block.Succs))
		for i, k := range block.Succs {
			switchBr.AddCase(llvm.ConstInt(idxVal.Type(), uint64(i), false), b.blocks[k.Block()])
		}
	default:
		base.FatalfAt(block.Pos, "unsupported BlockKind: %v", block.Kind)
	}
}

func (b *builder) createReturnByValue(block *ssa.Block, ret llvm.Value) {
	switch b.llvmFnType.ReturnType().TypeKind() {
	case llvm.VoidTypeKind:
		if !ret.IsUndef() {
			base.FatalfAt(block.Pos, "MakeResult must return undef for void type")
		}
		b.CreateRetVoid()
	default:
		if ret.IsUndef() {
			base.FatalfAt(block.Pos, "MakeResult cannot return undef for non-void type")
		}
		b.CreateRet(ret)
	}
}

func nameXPos(n *ir.Name) src.XPos {
	if n.Func == nil {
		return src.NoXPos
	}
	return n.Func.Pos()
}

func (b *builder) state() *state {
	return b.s
}

func (b *builder) curfn() *ir.Func {
	return b.s.curfn
}

func (b *builder) checkFeasibility() string {
	curfn := b.curfn()

	if isDenylisted(b.functag) {
		return "denylisted"
	}

	if !isPackageAllowed(curfn.LSym.Pkg) || isPackageDisallowed(curfn.LSym.Pkg) {
		return "package denylisted"
	}

	if strings.HasSuffix(b.s.f.Name, "init") {
		return "TODO: ignore init"
	}

	if curfn.ABIWrapper() {
		return "no ABI wrapper"
	}
	if curfn.ABI != obj.ABIInternal {
		return "must be ABI internal"
	}

	if curfn.LSym.CFunc() {
		// see obj6.go: stacksplit
		return "TODO: CFunc has different stack guard offset"
	}

	if curfn.LSym.Func().FuncID != 0 /* internal/abi.FuncIDNormal */ {
		return "TODO: be conservative about non-normal functions"
	}

	if curfn.LSym.Func().FuncFlag != 0 {
		return "TODO: be conservative about functions with specical flags"
	}

	if base.Flag.Cfg.BeastOpt.LLVM < 3 {
		// ignore runtime
		if base.Flag.CompilingRuntime || curfn.LSym.Pkg == "runtime" {
			return "-beast=2: no runtime package"
		}
	}

	if curfn.ABI != obj.ABIInternal {
		return "only ABIInternal is supported and ABI0 is not supported"
	}

	f := b.state().f
	if b.state().hasdefer || b.state().hasOpenDefers {
		return "TODO: Defer"
	}

	isNotInHeapTy := func(t *types.Type) bool {
		// ptr or int
		if t.Kind() == types.TPTR {
			return t.Elem().NotInHeap()
		}
		return t.NotInHeap()
	}

	for _, block := range f.Blocks {
		for _, instr := range block.Values {
			switch instr.Op {
			case ssa.OpAMD64MOVQi2f:
				// see intrinsics.go
				return "TODO: amd64 intrinsics is not supported yet"
			case ssa.OpTailCall:
				return "tail call optimization is not enabled for Go ABI internal"
			case ssa.OpKeepAlive, ssa.OpVarLive:
				return "TODO: KeepAlive or VarLive"
			case ssa.OpConvert:
				if base.Flag.Cfg.BeastOpt.OPConvert > 0 {
					from := isNotInHeapTy(instr.Args[0].Type)
					to := isNotInHeapTy(instr.Type)
					if (from || to) && !(from && to) {
						return "unsafe NotInHeap convert"
					}
				} else {
					return "TODO: unsafe convert is not guaranteed"
				}
			case ssa.OpFloor, ssa.OpFMA, ssa.OpTrunc, ssa.OpCeil:
				return "TODO: libm is not supported"
			}
			if aux, ok := instr.Aux.(*ssa.AuxCall); ok {
				if aux.Fn != nil {
					if aux.Fn.ABI() != obj.ABIInternal {
						// return "only ABIInternal is supported and ABI0 is not supported"
					}
					if aux.Fn.ABIWrapper() {
						// return "no ABI wrapper"
					}
				}
				continue
			}
			if aux, ok := instr.Aux.(*obj.LSym); ok {
				if aux.Type == objabi.STEXT && (aux.ABIWrapper() || aux.ABI() != obj.ABIInternal) {
					return "no ABI wrapper or ABI0"
				}
				if aux.ABI() != obj.ABIInternal {
					// workaround
					// TODO: a rare case:
					// 1). A function is defined in assembly and has ABI0
					// 2). The ABI0 is used by abi.FuncPCABI0 so the sym is created by direct lookup
					// See getGoFunctionLVMName. We could use a unified naming schema for all symbols.
					sym := base.Ctxt.LookupLLVMGoSymbolABI(aux.Name, obj.ABIInternal)
					// If a named symbol has an ABI internal symbol, then the symbol must point to a function.
					if sym != nil {
						return "oops, an assembly function's direct address is used via abi.FuncPCABI0"
					}
				}
				continue
			}
		}
	}

	return ""
}

// checked FrameOffset
// FrameOffset of PAUTO is unavaiable when bulding LLVM IR.
func (b *builder) FrameOffset(n *ir.Name) int64 {
	switch n.Class {
	case ir.PPARAM, ir.PPARAMOUT:
		// assigned via ABI
		return n.FrameOffset()
	}
	b.Fatalf("cannot get frame offset for non-param: %v", n)
	return -1
}

// createFunction builds the LLVM IR implementation for this function.
func (b *builder) BuildLLVM() {
	if base.Flag.Cfg.BeastOpt.LLVM == 0 {
		return
	}
	if b == nil {
		return
	}

	// FIXME: skip wrapper properly
	if b.curfn().LSym.Wrapper() {
		return
	}

	if b.curfn().ABIWrapper() {
		return
	}

	if b.curfn().ABI != obj.ABIInternal {
		return
	}

	if base.Flag.Cfg.BeastOpt.LLVMMaxFuncCount > 0 && b.summary.optedFuncs > base.Flag.Cfg.BeastOpt.LLVMMaxFuncCount {
		return
	}

	if base.Flag.Cfg.BeastOpt.LLVM >= 2 && base.Flag.Cfg.BeastOpt.LLVM <= 3 {
		if r := b.checkFeasibility(); r != "" {
			if b.Verbose > 1 {
				fmt.Printf("%s: skip for %s\n", b.functag, r)
			}
			return
		}
		if b.Verbose > 1 {
			fmt.Printf("%s: will use LLVM\n", b.functag)
		}
	}

	b.summary.optedFuncs += 1

	b.Builder = b.ctx.NewBuilder()
	defer b.Builder.Dispose()

	if b.DumpSSA {
		fmt.Fprintf(os.Stderr, "%s: %v\n", b.functag, b.f.Type)
	}
	b.createFunctionStart()
	// Fill blocks with instructions.
	postorder := b.f.Postorder()
	if len(postorder) == 0 {
		panic("No blocks in this function")
	}
	if b.f.Entry != postorder[len(postorder)-1] {
		panic("The single root block must be the last one of postorder")
	}

	// mark functions with interface calls as not inlinable
	for _, r := range b.curfn().LSym.R {
		switch r.Type {
		case objabi.R_USEFIELD, objabi.R_USEIFACE, objabi.R_USETYPE, objabi.R_USEIFACEMETHOD, objabi.R_USENAMEDMETHOD:
			// TODO: support inlining interface calls by propagating
			// objabi.R_USEIFACE, objabi.R_USEIFACEMETHOD, objabi.R_USEGENERICIFACEMETHOD reloations
			b.inlinable = false
		}
	}

	for i := len(postorder) - 1; i >= 0; i-- {
		block := postorder[i]
		llvmBlock := b.ctx.AddBasicBlock(b.llvmFn, block.String())
		b.blocks[block] = llvmBlock
	}

	// create entry block so that we can create alloca.
	entryLLVMBlock := b.blocks[b.f.Entry]
	b.SetInsertPointAtEnd(entryLLVMBlock)
	{
		// create mapping of locals
		maxargs := int64(-1)
		for _, b := range b.f.Blocks {
			for _, v := range b.Values {
				if _, ok := v.Aux.(*ssa.AuxCall); ok {
					if maxargs < v.AuxInt {
						maxargs = v.AuxInt
					}
					continue
				}
				switch v.Op {
				case ssa.OpPanicBounds:
					n := int64(2 * types.PtrSize)
					if maxargs < n {
						maxargs = n
					}
				}
			}
		}

		for _, n := range b.curfn().Dcl {
			switch n.Class {
			case ir.PPARAMOUT:
				if !n.IsOutputParamInRegisters() {
					size := n.Type().Size()
					if size == 0 {
						continue
					}
					offset := b.FrameOffset(n)
					et := b.getLLVMType(n.Type())
					addr := b.createGoPreAllocated(et, llvm.ConstInt(b.int64Type, uint64(offset), false),
						llvm.ConstInt(b.int64Type, uint64(size), false),
						b.getZeroValueConst(et),
						llvm.ConstInt(b.int1Type, uint64(1), false))
					b.inlinable = false
					b.locals[n] = addr
					continue
				}
				fallthrough
			case ir.PAUTO:
				if n.Sym() == nil || n.Sym().Name == "" {
					base.FatalfAt(nameXPos(n), "n must have a name when it is used in function body")
					return
				}

				a := b.CreateAlloca(b.getLLVMType(n.Type()), n.Sym().Name)
				b.addStackObjectGCData(n, a)
				b.locals[n] = a
			}
		}

		{
			mdid := b.ctx.MDKindID("go.stackobject.gcdata")
			var mdarray []llvm.Metadata
			for _, n := range b.curfn().Dcl {
				switch n.Class {
				case ir.PPARAMOUT:
					if n.IsOutputParamInRegisters() {
						// use alloca for such PARAMOUT
						continue
					}
					// see (*ABIConfig).updateOffset
					fallthrough
				case ir.PPARAM:
					if n.Esc() != ir.EscHeap && n.Type().HasPointers() && n.Addrtaken() {
						// stack objects
						t := n.Type()
						sz := t.Size()
						lsym, ptrdata := reflectdata.GCSym(t, false)
						// only param and paramout have offset now
						mdoffset := llvm.ConstInt(b.int32Type, uint64(n.FrameOffset()), false).ConstantAsMetadata()
						mdsize := llvm.ConstInt(b.int32Type, uint64(sz), false).ConstantAsMetadata()
						mdptrdata := llvm.ConstInt(b.int32Type, uint64(ptrdata), false).ConstantAsMetadata()
						mdsym := b.ctx.MDString(lsym.Name)
						mdarray = append(mdarray, mdoffset, mdsize, mdptrdata, mdsym)
					}
				}
			}
			if len(mdarray) != 0 {
				b.llvmFn.GlobalSetMetadata(mdid, b.ctx.MDNode(mdarray))
			}
		}

		// create argmap area
		abiInfo := b.f.OwnAux.ABIInfo()
		if abiInfo.ArgWidth() != 0 {
			mdid := b.ctx.MDKindID("go.caller.args.area")
			mdsize := llvm.ConstInt(b.int64Type, uint64(types.RoundUp(abiInfo.ArgWidth(), int64(types.RegSize))), false).ConstantAsMetadata()
			mdarray := []llvm.Metadata{mdsize, mdsize /* placeholder */}
			pcsize := int64(8) // size for return address
			for _, pa := range abiInfo.InParams() {
				pts, pos := pa.RegisterTypesAndOffsets()
				if len(pts) == 0 {
					continue
				}
				if len(pts) != len(pos) {
					panic("Mismatched registers and register types")
				}
				if len(pts) != len(pa.Registers) {
					panic("Mismatched registers and register types")
				}
				fo := pa.FrameOffset(abiInfo)
				for i := 0; i < len(pts); i++ {
					op, idx := ssa.ArgOpAndRegisterFor(pa.Registers[i], b.f.ABISelf)
					if idx < 0 {
						panic("negative reg index")
					}
					idx = idx + 1 // 0 == - 0, backend must decrement idx by one
					if op == ssa.OpArgFloatReg {
						idx = -idx
					}
					// a triple: (regidx, frame offset, size)
					mdarray = append(mdarray, llvm.ConstInt(b.int64Type, uint64(idx), false).ConstantAsMetadata())
					mdarray = append(mdarray, llvm.ConstInt(b.int64Type, uint64(fo+pos[i]+pcsize), false).ConstantAsMetadata())
					mdarray = append(mdarray, llvm.ConstInt(b.int64Type, uint64(pts[i].Size()), false).ConstantAsMetadata())
					isPointer := uint64(0)
					if pts[i].HasPointers() {
						isPointer = 1
					}
					mdarray = append(mdarray, llvm.ConstInt(b.int64Type, isPointer, false).ConstantAsMetadata())
				}
			}
			b.llvmFn.GlobalSetMetadata(mdid, b.ctx.MDNode(mdarray))
		}
		{
			for _, block := range b.f.Blocks {
				for _, instr := range block.Values {
					switch instr.Op {
					default:
						break
					case ssa.OpClosureCall, ssa.OpClosureLECall:
						b.llvmFn.GlobalSetMetadata(b.ctx.MDKindID("go.has.closurecall"), b.ctx.MDNode([]llvm.Metadata{}))
						break
					}
				}
			}
		}
		if maxargs != 0 {
			maxargs = types.RoundUp(maxargs, int64(types.RegSize))
			mdid := b.ctx.MDKindID("go.callee.args.area")
			mdsize := llvm.ConstInt(b.int64Type, uint64(maxargs), false).ConstantAsMetadata()
			b.llvmFn.GlobalSetMetadata(mdid, b.ctx.MDNode([]llvm.Metadata{mdsize, mdsize /* placeholder*/}))
		}
		{
			mdid := b.ctx.MDKindID("go.funcinfo")
			fn := b.f.OwnAux.Fn
			mdargs := llvm.ConstInt(b.int32Type, uint64(types.RoundUp(abiInfo.ArgWidth(), int64(types.RegSize))), false).ConstantAsMetadata()
			mdfuncid := llvm.ConstInt(b.int8Type, uint64(fn.Func().FuncID), false).ConstantAsMetadata()
			mdfuncflag := llvm.ConstInt(b.int8Type, uint64(fn.Func().FuncFlag), false).ConstantAsMetadata()
			mdstartline := llvm.ConstInt(b.int32Type, uint64(fn.Func().StartLine), false).ConstantAsMetadata()
			b.llvmFn.GlobalSetMetadata(mdid, b.ctx.MDNode([]llvm.Metadata{mdargs, mdfuncid, mdfuncflag, mdstartline}))
		}
	}

	// create block
	b.createBlock(b.f.Entry, entryLLVMBlock)

	for i := len(postorder) - 2; i >= 0; i-- {
		block := postorder[i]
		llvmBlock := b.blocks[block]
		b.createBlock(block, llvmBlock)
	}
	// reorder the blocks to be the original input order
	prevBlock := b.f.Entry
	for _, blk := range b.f.Blocks {
		if blk == b.f.Entry {
			continue
		}
		if llvmBlock, ok := b.blocks[blk]; ok {
			llvmBlock.MoveAfter(b.blocks[prevBlock])
			prevBlock = blk
		}
	}

	b.reachable = ssa.ReachableBlocks(b.f)
	// iterate over all blocks to update PHI
	for _, block := range postorder {
		for _, v := range block.Values {
			if v.Op == ssa.OpPhi {
				b.completePhi(block, v)
			}
		}
	}

	// add metadata for go.stack.param
	// TODO: merge into go.caller.args.area
	{
		var mdarray []llvm.Metadata
		for n, v := range b.locals {
			if n.Class != ir.PPARAM {
				continue
			}
			if !v.IsAAllocaInst().IsNil() {
				continue
			}
			if v.IsAIntrinsicInst().IsNil() {
				panic("must be alloca or intrinsicall")
			}
			if !n.Type().HasPointers() {
				continue
			}
			t := b.getLLVMType(n.Type())
			mtype := b.getZeroValueMeta(t)
			offset := b.FrameOffset(n)
			if offset < 0 {
				panic("must be preallocated")
			}
			// no need to add pcsize since the offset is relative to frame.argp
			moffset := llvm.ValueAsMetadata(llvm.ConstInt(b.int64Type, uint64(offset), false))
			mdarray = append(mdarray, mtype, moffset)
		}
		if len(mdarray) > 0 {
			mdid := b.ctx.MDKindID("go.stack.param")
			b.llvmFn.GlobalSetMetadata(mdid, b.ctx.MDNode(mdarray))
		}
	}

	b.addStandardDefinedAttributes(b.llvmFn)

	if base.Flag.Cfg.BeastOpt.AMD64TargetFeature > 0 {
		b.addTargetFeatureAttributes(b.llvmFn)
	}
}

func (b *builder) memUse(v *ssa.Value) *ssa.Value {
	// Phi and InitMem can be viewed as the start mem of each block
	if v.Op == ssa.OpPhi || v.Op == ssa.OpInitMem {
		return nil
	}
	return v.MemoryArg()
}

func (b *builder) memDef(v *ssa.Value) *ssa.Value {
	if v.Type.IsMemory() {
		return v
	}
	return nil
}

// -1: x < y, x happens before y
// 0: unordered
// 1: x > y, x happens after y
func (b *builder) checkMemOrder(x, y *ssa.Value) int {
	if x == y {
		return 0
	}

	recordCache := func(x, y *ssa.Value, o int) {
		if o == 0 {
			panic("only cache ordered pair")
		}
		if b.memOrderCache == nil {
			b.memOrderCache = make(map[[2]*ssa.Value]int)
		}
		if x.ID > y.ID {
			b.memOrderCache[[2]*ssa.Value{x, y}] = o
		} else {
			b.memOrderCache[[2]*ssa.Value{y, x}] = -o
		}
	}

	checkCache := func(x, y *ssa.Value) int {
		if x.ID > y.ID {
			if o, ok := b.memOrderCache[[2]*ssa.Value{x, y}]; ok {
				return o
			}
		} else {
			if o, ok := b.memOrderCache[[2]*ssa.Value{y, x}]; ok {
				return -o
			}
		}
		return 0
	}

	if o := checkCache(x, y); o != 0 {
		return o
	}

	// x and y must use mem except one of them is Phi or InitMem
	// x and y may not define mem
	xu, yu := b.memUse(x), b.memUse(y)
	if xu == nil {
		if yu == nil {
			panic("two init mem")
		}
		return -1
	}
	if yu == nil {
		if xu == nil {
			panic("two init mem")
		}
		return 1
	}

	// invariant: xu and yu are not nil

	xd, yd := b.memDef(x), b.memDef(y)
	if xd != nil && yd != nil {
		// both are def and they must be ordered
		for xu != nil && yu != nil {
			xu = b.memUse(xu)
			yu = b.memUse(yu)
		}
		if xu != nil {
			// x greater than y
			// x happens after y
			recordCache(x, y, 1)
			return 1
		}
		if yu != nil {
			recordCache(x, y, -1)
			return -1
		}
		panic(fmt.Errorf("%s and %s must be ordered", x, y))
	}

	if xd == nil && yd == nil {
		// xu and yu must be ordered or the same
		return b.checkMemOrder(xu, yu)
	}

	if xd == nil {
		o := b.checkMemOrder(xu, yd)
		switch o {
		case -1:
			// xu happens before yd
			// then x must happen before y otherwise xu is overridden by yd
			return -1
		case 0:
			// xu == yd
			// then x must happen after y otherwise xu/yd is not defined
			return 1
		case 1:
			// xu happens after y/yd
			return 1
		default:
			panic("oops")
		}
	}

	if yd == nil {
		o := b.checkMemOrder(yu, xd)
		switch o {
		case -1:
			// yu happens before xd
			// then y must happen before x/xd otherwise yu is overridden by xd
			// x happen after y
			return 1
		case 0:
			// xd == yu
			// then x must happen before y otherwise xd/yu is not defined
			return -1
		case 1:
			// yu happens after x/xd
			return -1
		default:
			panic("oops")
		}
	}

	panic("all cases must be handled")
}

func (b *builder) addGoTypeMetadata(v llvm.Value, t llvm.Type) {
	mdid := b.ctx.MDKindID("go.type")
	v.SetMetadata(mdid, b.ctx.MDNode([]llvm.Metadata{b.getZeroValueMeta(t)}))
}

// a PHI node has n args
func (b *builder) completePhi(block *ssa.Block, phi *ssa.Value) {
	if len(block.Preds) != len(phi.Args) {
		base.FatalfAt(block.Pos, "bad PHI node: %v", phi)
	}
	llvmPHI := b.getValue(phi)
	if llvmPHI.IsNil() {
		return // A phi only has memory arguments.
	}
	phiType := llvmPHI.Type()
	incomingCount := len(block.Preds)
	incomingVals := make([]llvm.Value, 0, incomingCount)
	incomingBlocks := make([]llvm.BasicBlock, 0, incomingCount)
	for i := 0; i < incomingCount; i++ {
		gblock := block.Preds[i].Block()
		if !b.reachable[gblock.ID] {
			continue
		}
		value := b.getValue(phi.Args[i])
		lblock := b.blocks[gblock]
		valType := value.Type()
		if valType != phiType {
			pos := lblock.LastInstruction()
			b.SetInsertPointBefore(pos)
			value = b.extendedBitcast(value, phiType)
		}
		incomingVals = append(incomingVals, value)
		incomingBlocks = append(incomingBlocks, lblock)
	}
	llvmPHI.AddIncoming(incomingVals, incomingBlocks)
	return
}

func (b *builder) makeCall(t llvm.Type, callee llvm.Value, args []llvm.Value, cconv int, name string) llvm.Value {
	call := b.CreateCall(t, callee, args, name)

	for i, arg := range args {
		if arg.Type().TypeKind() == llvm.IntegerTypeKind && arg.Type().IntTypeWidth() == 1 {
			// zero extend i1
			call.AddCallSiteAttribute(1+i, b.ctx.CreateEnumAttribute(llvm.AttributeKindID("zeroext"), 0))
		}
	}
	if cconv != 0 {
		call.SetInstructionCallConv(llvm.CallConv(cconv))
	}
	return call
}

// createInstruction builds the LLVM IR equivalent instructions for the
// particular Go SSA instruction.
func (b *builder) createInstruction(instr *ssa.Value) {
	oldpos := b.setCurLoc(instr.Pos)
	defer b.setCurLoc(oldpos)
	if b.DumpSSA {
		last := b.Builder.GetInsertBlock().LastInstruction()
		defer func() {
			fmt.Fprintf(os.Stderr, "%s: %s\n", b.functag, instr.LongString())
			bb := b.Builder.GetInsertBlock()
			var ii llvm.Value
			if last.IsNil() {
				ii = bb.FirstInstruction()
			} else {
				ii = llvm.NextInstruction(last)
			}
			for !ii.IsNil() {
				fmt.Fprintf(os.Stderr, "%s: ", b.functag)
				ii.Dump()
				fmt.Fprintln(os.Stderr)
				ii = llvm.NextInstruction(ii)
			}
		}()
	}
	// Every SSA value is a statement and has a corresponding LLVM IR instructions.
	// Even constant will be stored into a variable first?
	// Or we can skip create LLVM IR for constants and embed constants at use site.
	// We deal ssa.Value of a Void type here and process other ssa.Value in createExpr.
	// The result is cached in b.values
	switch instr.Op {
	case ssa.OpStore:
		// stmt
		addr := b.getValue(instr.Args[0])
		val := b.getValue(instr.Args[1])
		addr = b.toPtr(addr, llvm.PointerType(val.Type(), 0))
		b.CreateStore(val, addr)
	case ssa.OpMove:
		dstptr := b.getValue(instr.Args[0])
		srcptr := b.getValue(instr.Args[1])
		auxt := instr.Aux.(*types.Type)
		if dstptr.Type() != b.i8ptrType {
			dstptr = b.toPtr(dstptr, b.i8ptrType)
		}
		if srcptr.Type() != b.i8ptrType {
			srcptr = b.toPtr(srcptr, b.i8ptrType)
		}
		size := llvm.ConstInt(b.int64Type, uint64(instr.AuxInt), false)
		align := int(auxt.Alignment())
		b.CreateMemCpyInline(dstptr, align, srcptr, align, size)
	case ssa.OpZero:
		ptr := b.getValue(instr.Args[0])
		if ptr.Type() != b.i8ptrType {
			ptr = b.toPtr(ptr, b.i8ptrType)
		}
		auxt := instr.Aux.(*types.Type)
		size := llvm.ConstInt(b.int64Type, uint64(instr.AuxInt), false)
		val := llvm.ConstInt(b.int8Type, 0, false)
		align := int(auxt.Alignment())
		b.CreateMemSet(ptr, val, size, align)
	case ssa.OpPanicBounds:
		kind := instr.AuxInt
		sym := BoundsCheckFunc[kind]
		b.createGoCallWithArgs(sym, []llvm.Value{b.getValue(instr.Args[0]), b.getValue(instr.Args[1])}, b.ctx.VoidType())
	case ssa.OpInlMark:
		// We can't use intrinsic here,
		// because "Remove dead machine instructions" will delete this instruction.
		fnType := llvm.FunctionType(b.ctx.VoidType(), []llvm.Type{}, false)
		target := llvm.InlineAsm(fnType, "nop", "", false, false,
			llvm.InlineAsmDialectATT, false)
		b.CreateCall(fnType, target, []llvm.Value{}, "")
	case ssa.OpPubBarrier:
		// x86 doesn't need it, but arm need.
		// We only support x86 now, so we do nothing here.
	case ssa.OpInitMem, ssa.OpSB:
		// noop
	case ssa.OpWBend:
		// TODO: used to mark UnsafePoint
	case ssa.OpVarDef:
	case ssa.OpVarLive, ssa.OpKeepAlive:
		//
	case ssa.OpPrefetchCache:
		// Nop
		addr := b.getValue(instr.Args[0])
		b.createIntrinsicCallWithArgs("llvm.prefetch.p0", []llvm.Value{
			b.toPtr(addr, b.i8ptrType),
			llvm.ConstInt(b.int32Type, 0, false), // 0 for read
			llvm.ConstInt(b.int32Type, 3, false), // 3 for all cache prefetcht0
			llvm.ConstInt(b.int32Type, 1, false), // 1 for data cache
		}, b.voidType)
	case ssa.OpPrefetchCacheStreamed:
		// Nop
		addr := b.getValue(instr.Args[0])
		b.createIntrinsicCallWithArgs("llvm.prefetch.p0", []llvm.Value{
			b.toPtr(addr, b.i8ptrType),
			llvm.ConstInt(b.int32Type, 0, false), // 0 for read
			llvm.ConstInt(b.int32Type, 0, false), // 0 for no cache: prefetchnta
			llvm.ConstInt(b.int32Type, 1, false), // 1 for data cache
		}, b.voidType)
	case ssa.OpConst8, ssa.OpConst16, ssa.OpConst32, ssa.OpConst64, ssa.OpConst32F, ssa.OpConst64F, ssa.OpConstBool,
		ssa.OpConstNil, ssa.OpConstString, ssa.OpConstInterface, ssa.OpConstSlice, ssa.OpSP, ssa.OpInvalid, ssa.OpUnknown:
		// Constants are created on-demand. See getValue
		// OpSP is a constant
	default:
		expr := b.createExpr(instr)
		if expr.IsNil() {
			if !instr.Type.IsMemory() {
				// We may nullify an SSA instruction if it only operates on mem arguments
				// or it is a Call with Void return type.
				// For other cases, it must be a bug.
				base.FatalfAt(instr.Pos, "oops: no value is created for %v", instr)
			}
		} else {
			if _, ok := b.values[instr]; ok {
				base.FatalfAt(instr.Pos, "oops: value has been created for %v", instr)
			}

			switch instr.Op {
			case ssa.OpMakeResult:
				break
			case ssa.OpMul64uover:
				// TODO: OpMul64uover should return uintptr,bool. The ssagen use uintptr,uintptr as the return value.
				// We should report this kind of discrepancy to upstream.
			default:
				toType := b.getLLVMType(instr.Type)
				expr = b.patchValueType(expr, toType)
			}
			b.values[instr] = expr
		}
	}
}

func (b *builder) hasPointer(t llvm.Type) bool {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		for _, et := range t.StructElementTypes() {
			if b.hasPointer(et) {
				return true
			}
		}
		return false
	case llvm.ArrayTypeKind:
		return b.hasPointer(t.ElementType())
	case llvm.IntegerTypeKind:
		return false
	case llvm.PointerTypeKind:
		return true
	case llvm.FloatTypeKind, llvm.DoubleTypeKind:
		return false
	}
	return false
}

func (b *builder) getZeroValueConst(t llvm.Type) llvm.Value {
	switch t.TypeKind() {
	case llvm.StructTypeKind, llvm.ArrayTypeKind:
		return llvm.ConstAggregateZero(t)
	case llvm.IntegerTypeKind:
		return llvm.ConstInt(t, 0, false)
	case llvm.PointerTypeKind:
		return llvm.ConstNull(t)
	case llvm.FloatTypeKind, llvm.DoubleTypeKind:
		return llvm.ConstFloat(t, 0)
	}
	b.Fatalf("cannot get zero value as metadata for type %v", t)
	return llvm.Value{}
}

func (b *builder) getZeroValueMeta(lt llvm.Type) llvm.Metadata {
	switch lt.TypeKind() {
	case llvm.StructTypeKind, llvm.ArrayTypeKind:
		return llvm.ValueAsMetadata(llvm.ConstAggregateZero(lt))
	case llvm.IntegerTypeKind:
		return llvm.ConstInt(lt, 0, false).ConstantAsMetadata()
	case llvm.PointerTypeKind:
		return llvm.ConstNull(lt).ConstantAsMetadata()
	case llvm.FloatTypeKind, llvm.DoubleTypeKind:
		return llvm.ConstFloat(lt, 0).ConstantAsMetadata()
	}
	b.Fatalf("cannot get zero value as metadata for type %v", lt)
	return llvm.Metadata{}
}

func (b *builder) patchValueType(fromVal llvm.Value, toType llvm.Type) llvm.Value {
	if !fromVal.IsNil() && !toType.IsNil() && fromVal.Type() != toType {
		// try bitcast
		// TODO: optimze
		casted := b.extendedBitcast(fromVal, toType)
		if casted.IsNil() {
			fromType := fromVal.Type()
			// try truncate
			// TODO: Go SSA is not strictly typed.
			// Rsh64x64 may return 32bit
			// Const64 may return 32bit
			if toType.TypeKind() == llvm.IntegerTypeKind && fromType.TypeKind() == llvm.IntegerTypeKind {
				if toType.IntTypeWidth() < fromType.IntTypeWidth() {
					casted = b.CreateTrunc(fromVal, toType, "")
				}
			}
		}
		if !casted.IsNil() {
			return casted
		}
	}
	return fromVal
}

func (b *builder) createGoCall(sym *obj.LSym, expr *ssa.Value) llvm.Value {
	args := make([]llvm.Value, 0, len(expr.Args))
	for _, arg := range expr.Args {
		if arg.Type == types.TypeMem {
			continue
		}
		llvmArg := b.getValue(arg)
		args = append(args, llvmArg)
	}
	return b.createGoCallWithArgs(sym, args, b.getLLVMType(expr.Type))
}

func (b *builder) setupCallingConvention(v llvm.Value, abi obj.ABI) {
	if !v.IsAFunction().IsNil() {
		if abi == obj.ABI0 {
			v.SetFunctionCallConv(llvm.CallConv(llvm.GO_ABI0_CCONV))
		} else {
			v.SetFunctionCallConv(llvm.CallConv(llvm.GO_ABIINTERNAL_CCONV))
		}
		return
	}
	if !v.IsACallInst().IsNil() || !v.IsAInvokeInst().IsNil() {
		if abi == obj.ABI0 {
			v.SetInstructionCallConv(llvm.CallConv(llvm.GO_ABI0_CCONV))
			// TODO: remove fatal if we can support ABI0 in LLVM backend.
		} else {
			v.SetInstructionCallConv(llvm.CallConv(llvm.GO_ABIINTERNAL_CCONV))
		}
		return
	}
	b.Fatalf("Any other instruction needs a calling convention?")
}

func (c *compilerContext) setupMetadata(llvmFn llvm.Value, goSym *obj.LSym) {
	// use the package of the function since the function may be declared in another package.
	md := c.ctx.MDNode([]llvm.Metadata{c.ctx.MDString(goSym.Pkg)})
	llvmFn.GlobalSetMetadata(30, md) // annotation
	switch goSym.Name {
	case "runtime.newobject", "runtime.makemap", "runtime.makemap_small",
		"runtime.makeslice", "runtime.mallocgc", "runtime.mallocgc1",
		"runtime.makechan":
		llvmFn.AddAttributeAtIndex(0, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nonnull"), 0))
	}
	dupok := "false"
	if goSym.DuplicateOK() {
		dupok = "true"
	}
	llvmFn.GlobalSetMetadata(c.ctx.MDKindID("go.function.dupok"), c.ctx.MDNode([]llvm.Metadata{c.ctx.MDString(dupok)}))
}

func (b *builder) getOrCreateGoFunction(sym *obj.LSym, paramTypes []llvm.Type, retType llvm.Type) (llvm.Value, llvm.Type) {
	name := b.getGoFunctionLLVMName(sym)
	var llvmFnType llvm.Type
	llvmFn := b.mod.NamedFunction(name)
	if llvmFn.IsNil() {
		tsym := findRuntimeBuiltinSymbol(sym)
		if tsym != nil {
			llvmFnType = b.getDefultSubstitutionFunctionType(tsym, sym, paramTypes, retType)
		} else {
			// At SSA level, no function is vararg.
			llvmFnType = llvm.FunctionType(retType, paramTypes, false /* TODO */)
		}

		g := b.mod.NamedGlobal(name)
		if !g.IsNil() {
			g.SetName(g.Name() + "..obsolete")
		}

		llvmFn = llvm.AddFunction(b.mod, name, llvmFnType)
		if llvmFn.Name() != name {
			b.mod.Dump()
			panic("cannot add function with desired name")
		}

		if !g.IsNil() {
			f := b.patchValueType(llvmFn, g.Type())
			g.ReplaceAllUsesWith(f)
		}
		if sym.DuplicateOK() {
			b.dupokFunc[name] = true
		}
		// All functions are external linkage.
		// The linkage type does not matter since the object file generated by LLVM
		// is a temporal container.
		llvmFn.SetLinkage(llvm.ExternalLinkage)
		b.setupMetadata(llvmFn, sym)
		b.setupCallingConvention(llvmFn, sym.ABI())
	} else {
		llvmFnType = llvmFn.FunctionType()
	}
	return llvmFn, llvmFnType
}

func findRuntimeBuiltinSymbol(sym *obj.LSym) *types.Sym {
	var localname string
	pkg := ir.Pkgs.Runtime
	if sym.Pkg == "runtime" {
		idx := strings.LastIndex(sym.Name, ".")
		if idx == -1 {
			panic("no dot in symbol name: " + sym.Name)
		}
		localname = sym.Name[idx+1:]
		if base.Ctxt.Pkgpath == sym.Pkg {
			pkg = types.LocalPkg
		}
	} else if sym.Pkg == "_" && strings.HasPrefix(sym.Name, "runtime.") {
		localname = sym.Name[len("runtime."):]
	} else if base.Flag.CompilingRuntime {
		idx := strings.LastIndex(sym.Name, ".")
		if idx == -1 {
			panic("no dot in symbol name: " + sym.Name)
		}
		if sym.Name[0:idx] != "runtime" {
			return nil
		}
		localname = sym.Name[idx+1:]
	} else {
		return nil
	}

	if s, ok := pkg.Syms[localname]; ok {
		return s
	}
	return nil
}

// Some Go builtin functions have an overloading behavior. For example, newobject() can be used to allocate
// any type. This is supported by define the builtin function with any type in the compiler.
// See src/cmd/compile/internal/typecheck/builtin/runtime.go
//
// The any type is kind of type parameter that can be substituted with a type argument.
// The Go compiler can do such function overloading that is not supported by the Go language.
// In LLVM, function cannot be overloaded. Every function must have a unique name.
// We do not follow the intrinsic approach that adds a suffix to a LLVM intrinsic to support overloading.
// Instead, we only declare a single LLVM function for each builtin function, using the default substitution.
// The default substitution substitues any pointer type to i8*. This is because any Go type will be eventually
// lowered into a primitive, a pointer, a struct or an array type.
func (b *builder) getDefultSubstitutionFunctionType(sym *types.Sym, lsym *obj.LSym, paramTypes []llvm.Type, retType llvm.Type) llvm.Type {
	key := b.getGoFunctionLLVMName(lsym)
	if llvmType, ok := b.runtimeBuiltdinMap[key]; ok {
		return llvmType
	}
	newParamTypes := make([]llvm.Type, len(paramTypes))
	for i, pt := range paramTypes {
		newParamTypes[i] = b.getDefaultSubstitution(pt)
	}
	var newRetType llvm.Type
	switch retType.TypeKind() {
	case llvm.StructTypeKind:
		// a tuple created TRESULT
		retUpdated := false
		elements := make([]llvm.Type, retType.StructElementTypesCount())
		for i, et := range retType.StructElementTypes() {
			net := b.getDefaultSubstitution(et)
			if et != net {
				retUpdated = true
			}
			elements[i] = net
		}
		if retUpdated {
			newRetType = b.ctx.StructType(elements, false)
		} else {
			newRetType = retType
		}
	// no need to check array since there is no array any type.
	default:
		// void or primitive
		newRetType = b.getDefaultSubstitution(retType)
	}
	llvmType := llvm.FunctionType(newRetType, newParamTypes, false)
	b.runtimeBuiltdinMap[key] = llvmType
	return llvmType
}

func (b *builder) getDefaultSubstitution(t llvm.Type) llvm.Type {
	switch t.TypeKind() {
	case llvm.PointerTypeKind:
		return b.i8ptrType
	default:
		// t must be a primitive
		return t
	}
}

func (b *builder) isFakeInterfaceReceiver(t llvm.Type) bool {
	return b.fakeRecvType == t
}

func (b *builder) createGoCallWithArgs(sym *obj.LSym, args []llvm.Value, retType llvm.Type) llvm.Value {
	paramTypes := make([]llvm.Type, len(args))
	for i, a := range args {
		paramTypes[i] = a.Type()
	}
	llvmFn, llvmFnType := b.getOrCreateGoFunction(sym, paramTypes, retType)
	call := b.makeCall(llvmFnType, llvmFn, args, llvm.GO_ABIINTERNAL_CCONV, "")
	return call
}

// get args of a Go SSA instruciton and ignore any TypeMem arg
func (b *builder) getArgs(expr *ssa.Value) []llvm.Value {
	args := make([]llvm.Value, 0, len(expr.Args))
	for _, arg := range expr.Args {
		if arg.Type == types.TypeMem {
			continue
		}
		llvmArg := b.getValue(arg)
		args = append(args, llvmArg)
	}
	return args
}

func (b *builder) createIntrinsicCallWithArgs(name string, args []llvm.Value, retType llvm.Type) llvm.Value {
	llvmFn := b.mod.NamedFunction(name)
	paramTypes := make([]llvm.Type, len(args))
	for i, a := range args {
		paramTypes[i] = a.Type()
	}
	if llvmFn.IsNil() {
		llvmFnType := llvm.FunctionType(retType, paramTypes, false /* TODO */)
		llvmFn = llvm.AddFunction(b.mod, name, llvmFnType)
		llvmFn.SetLinkage(llvm.ExternalLinkage)
	}
	return b.CreateCall(llvmFn.FunctionType(), llvmFn, args, "")
}

// after inlining, `runtime.morestack` is no longer generated, which can cause
// some opportunities for preemption to be lost. In cases where asyncpreempt
// is not supported, this could lead to issues such as infinite loops.
// currently, we rely on experience or speculation to create a denylist
// to skip inlining
func (b *builder) checkInlineBlocklist(op ssa.Op, auxCall *ssa.AuxCall) {
	calleeBlocklist := map[string]struct{}{
		"runtime.mcall": {},
	}
	if auxCall != nil && auxCall.Fn != nil {
		callee := auxCall.Fn.Name
		if _, ok := calleeBlocklist[callee]; ok {
			b.inlinable = false
			return
		}
	}

	//opBlocklist := map[ssa.Op]struct{}{
	//	ssa.OpGetG: {},
	//}
	//if _, ok := opBlocklist[op]; ok {
	//	b.inlinable = false
	//	return
	//}

}

// createFunctionCall lowers a Go SSA call instruction (to a simple function,
// closure, function pointer, builtin, method, etc.) to LLVM IR, usually a call
// instruction.
//
// This is also where compiler intrinsics are implemented.
func (b *builder) createFunctionCall(call *ssa.Value) llvm.Value {
	// Try to call the function directly for trivially static calls.
	var callee llvm.Value
	var closure llvm.Value
	var abi = obj.ABIInternal
	aux := call.Aux.(*ssa.AuxCall)

	b.checkInlineBlocklist(ssa.OpInvalid, aux)

	firstArg := 0
	switch call.Op {
	case ssa.OpClosureCall, ssa.OpClosureLECall:
		callee = b.getValue(call.Args[0])
		closure = b.getValue(call.Args[1])
		firstArg = 2
	case ssa.OpStaticCall:
		if base.Flag.Cfg.BeastOpt.IntrinsifyMemmove > 0 {
			if fn := call.Aux.(*ssa.AuxCall).Fn; fn != nil && fn.Name == "runtime.memmove" {
				if len(call.Args) == 4 {
					dst := call.Args[0]
					dstptr := b.getValue(dst)
					if dstptr.Type() != b.i8ptrType {
						dstptr = b.toPtr(dstptr, b.i8ptrType)
					}
					dst_align := int(dst.Type.Alignment())
					src := call.Args[1]
					srcptr := b.getValue(src)
					if srcptr.Type() != b.i8ptrType {
						srcptr = b.toPtr(srcptr, b.i8ptrType)
					}
					src_align := int(src.Type.Alignment())
					size := b.getValue(call.Args[2])
					b.CreateMemMove(dstptr, dst_align, srcptr, src_align, size)
					return llvm.Value{}
				} else {
					base.FatalfAt(call.Pos, "unexpected IR shape around a runtime.memmove call")
				}
			}
		}
		fallthrough
	case ssa.OpStaticLECall, ssa.OpTailCall, ssa.OpTailLECall:
		// for a named function, we cannot simply use call.Type.Sym().Linkname.
		// We need to get the LSym since ABI may be part of symbol
		// TODO:
		abi = aux.Fn.ABI()
		callee, _ = b.getFunctionByAuxCallABI(aux)
	case ssa.OpInterCall, ssa.OpInterLECall:
		callee = b.getValue(call.Args[0])
		firstArg = 1
	default:
		shouldNotReachHere()
		return llvm.Value{}
	}

	if firstArg != 0 {
		// indirect call, we need cast the receiver from uintptr to pointer to function
		// fromType is the type of the callee expression
		// toType is the type of a function that can take the arguments and return the results.
		llvmFnType := b.getFunctionTypesByAuxCall(aux)
		switch callee.Type().TypeKind() {
		case llvm.IntegerTypeKind:
			// Must be uintptr
			if callee.Type() == b.uintptrType {
				// TODO: In my understanding, this is the only valid case.
				callee = b.CreateIntToPtr(callee, llvm.PointerType(llvmFnType, 0), "")
			} else {
				// oops
				base.FatalfAt(call.Pos, "callee %v has unexpected type: %v@%v@%v", aux, call.Type, call.Op, callee.Type().TypeKind())
				return llvm.Value{}
			}
		default:
			// oops, a LLVM function type?
			base.FatalfAt(call.Pos, "callee %v has unexpected type: %v", aux, callee.Type().TypeKind())
			return llvm.Value{}
		}
	}
	// check call args types: do pointer casts when necessary
	llvmFnType := b.getFunctionTypesByAuxCall(aux)
	llvmParamTypes := llvmFnType.ParamTypes()

	var argStorage [16]llvm.Value
	args := argStorage[:0]
	// load closureptr if needed
	if !closure.IsNil() {
		var storage [8]llvm.Type
		newParamTypes := append(storage[:0], closure.Type())
		newParamTypes = append(newParamTypes, llvmParamTypes...)
		closureFnType := llvm.FunctionType(llvmFnType.ReturnType(), newParamTypes, false)
		llvmFnType = closureFnType
		callee = b.CreateBitCast(callee, llvm.PointerType(closureFnType, 0), "")
		args = append(args, closure)
	}

	// create real args
	for i := firstArg; i < len(call.Args)-1; i++ {
		arg := b.getValue(call.Args[i])
		// do necessary ``bitcast'' for arguments
		llvmParamType := llvmParamTypes[i-firstArg]
		newarg := b.extendedBitcast(arg, llvmParamType)
		if newarg.IsNil() {
			base.FatalfAt(call.Pos, "mismatched argument type for %v at %d, expect %v, get %v", aux, i, llvmParamType, arg.Type())
			return llvm.Value{}
		}
		args = append(args, newarg)
	}

	// make call
	var cconv = llvm.GO_ABIINTERNAL_CCONV
	if abi == obj.ABI0 {
		cconv = llvm.GO_ABI0_CCONV
	}
	goCall := b.makeCall(llvmFnType, callee, args, cconv, "")
	if !closure.IsNil() {
		goCall.AddCallSiteFnAttribute(b.ctx.CreateStringAttribute("go.closurecall", ""))
	}
	patchClean := func() {
		ownABI := b.f.OwnAux.Fn.ABI()
		// logic from ssagen and obj6
		if ownABI == obj.ABIInternal && abi == obj.ABI0 {
			// TODO: avoid call this in `Plan 9`
			fnType := llvm.FunctionType(b.ctx.VoidType(), []llvm.Type{}, false)
			// TLS Offset: src/cmd/link/internal/ld/sym.go computeOffset
			target := llvm.InlineAsm(fnType, "xorps %xmm15, %xmm15", "~{xmm15}", true, false,
				llvm.InlineAsmDialectATT, false)
			b.CreateCall(fnType, target, []llvm.Value{}, "")
			if x86.CanUse1InsnTLS(base.Ctxt) {
				// MOVQ (TLS), r
				fnType := llvm.FunctionType(b.ctx.VoidType(), []llvm.Type{}, false)
				var target llvm.Value
				if buildcfg.GOOS == "linux" {
					target = llvm.InlineAsm(fnType, "movq %fs:runtime.tls_g@tpoff, %r14", "~{r14}", true, false, llvm.InlineAsmDialectATT, false)
				} else if buildcfg.GOOS == "darwin" {
					target = llvm.InlineAsm(fnType, "movq %gs:0x0000000000000030, %r14", "~{r14}", true, false, llvm.InlineAsmDialectATT, false)
				} else {
					panic("unsupported arch: " + buildcfg.GOOS)
				}
				b.CreateCall(fnType, target, []llvm.Value{}, "")
			} else {
				base.FatalfAt(call.Pos, "unsupported arch and OS: %s %s", buildcfg.GOARCH, buildcfg.GOOS)
			}
		}
	}

	// do pointer cast for results
	abiRetType := b.getFunctionRetTypeByAuxCall(aux)
	if abiRetType.TypeKind() == llvm.VoidTypeKind {
		patchClean()
		return goCall
	}
	retVal := b.extendedBitcast(goCall, abiRetType)
	patchClean()
	if retVal.IsNil() {
		base.FatalfAt(call.Pos, "mismatched return type for %v, expect %v, get %v", aux, abiRetType, llvmFnType.ReturnType())
		return llvm.Value{}
	}
	return retVal
}

func (b *builder) extendedBitcastNonFail(expr *ssa.Value, toType llvm.Type) llvm.Value {
	fromValue := b.getValue(expr)
	toValue := b.extendedBitcast(fromValue, toType)
	if toValue.IsNil() {
		base.FatalfAt(expr.Pos, "cannot bitcast %v to %v", fromValue.Type().String(), toType.String())
	}
	return toValue
}

// Do bitcast/ptrtoint/inttoptr in a single function
// Recursively bitcast each element of a struct type
func (b *builder) extendedBitcast(retValue llvm.Value, retType llvm.Type) llvm.Value {
	valType := retValue.Type()
	if valType == retType {
		return retValue
	}
	if valType.IsNil() {
		return retValue
	}
	if valType.TypeKind() != retType.TypeKind() {
		// cannot do cast between different kind.
		// here we can only do bitcast
		// One exception is to cast between uintptr and pointers.
		// TODO: do ptrtoint/inttoptr cast
		if valType.TypeKind() == llvm.PointerTypeKind && retType == b.uintptrType {
			// cast a pointer to uintptr
			return b.CreatePtrToInt(retValue, retType, "")
		} else if valType == b.uintptrType && retType.TypeKind() == llvm.PointerTypeKind {
			// cast a uintptr to pointer
			return b.CreateIntToPtr(retValue, retType, "")
		} else if valType.TypeKind() == llvm.VoidTypeKind && retType.TypeKind() != llvm.VoidTypeKind {
			return retValue
		}
		return llvm.Value{}
	}
	switch retType.TypeKind() {
	case llvm.PointerTypeKind:
		return b.CreateBitCast(retValue, retType, "")
	case llvm.StructTypeKind:
		if valType.StructElementTypesCount() != retType.StructElementTypesCount() {
			return llvm.Value{}
		}
		// A struct may contain a pointer.
		// We cannot do bitcast for struct types.
		s := llvm.Undef(retType)
		valTypeElements := valType.StructElementTypes()
		retTypeElements := retType.StructElementTypes()
		for i, vt := range valTypeElements {
			rt := retTypeElements[i]
			v := b.CreateExtractValue(retValue, i, "")
			if vt != rt {
				v = b.extendedBitcast(v, rt)
				if v.IsNil() {
					return llvm.Value{}
				}
			}
			s = b.CreateInsertValue(s, v, i, "")
		}
		return s
	default:
		// Oops, we cannot do cast for any other type.
		// Return nil to notify caller
		return llvm.Value{}
	}
}

// getValue returns the LLVM value of a constant, function value, global, or
// already processed SSA expression.
// NOTE: We need to create a value for a used/dep value first
func (b *builder) getValue(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpConst8, ssa.OpConst16, ssa.OpConst32, ssa.OpConst64, ssa.OpConst32F, ssa.OpConst64F, ssa.OpConstBool,
		ssa.OpConstNil, ssa.OpConstString, ssa.OpConstInterface, ssa.OpConstSlice:
		return b.createConst(expr)
	case ssa.OpInvalid, ssa.OpUnknown:
		return llvm.ConstNull(b.getLLVMType(expr.Type))
	case ssa.OpSP:
		// OpSP is a constant
		return b.createSP(expr)
	case ssa.OpPhi:
		if value, ok := b.values[expr]; ok {
			return value
		} else {
			// we create a new Phi on-demand
			b.createInstruction(expr)
			return b.values[expr]
		}
	case ssa.OpOffPtr:
		// generate SP offset on-demand
		// if the base is a constant, then the OffPtr is actually a constant.
		if expr.Args[0].Op == ssa.OpSP {
			// create the expr on-demand and never cache the result
			return b.createExpr(expr)
		}
		fallthrough
	default:
		// other (local) SSA value
		if value, ok := b.values[expr]; ok {
			if value.IsNil() {
				base.FatalfAt(expr.Pos, "%s: get a nil value for %v", b.f.Name, expr)
			}
			return value
		} else {
			// indicates a compiler bug
			base.FatalfAt(expr.Pos, "%s: local has not been parsed: %s", b.f.Name, expr.LongString())
			return llvm.Value{}
		}
	}
}

func (b *builder) signExtend(val llvm.Value, t llvm.Type) llvm.Value {
	if val.Type().IntTypeWidth() > t.IntTypeWidth() {
		panic("Oops")
	}
	if val.Type() == t {
		return val
	}
	return b.CreateSExtOrBitCast(val, t, "")
}

func (b *builder) unsignExtend(val llvm.Value, t llvm.Type) llvm.Value {
	if val.Type().IntTypeWidth() > t.IntTypeWidth() {
		panic("Oops")
	}
	if val.Type() == t {
		return val
	}
	return b.CreateZExtOrBitCast(val, t, "")
}

func (b *builder) createSP(expr *ssa.Value) llvm.Value {
	// return the value of RSP
	// TODO: use llvm intrinsic. However, the generated code still move rsp to rbx and use rbx to do following computation.
	// TODO: we should allocate a local variable for the stack args area and use rbp to access each slot?
	rsp := b.ctx.MDNode([]llvm.Metadata{b.ctx.MDString("rsp")})
	// sp is assumed to be constant in Go
	call := b.createIntrinsicCallWithArgs("llvm.read_register.i64", []llvm.Value{llvm.MetadataAsValue(b.ctx, rsp)}, b.int64Type)
	b.inlinable = false
	return call
}

// createExpr translates a Go SSA expression to LLVM IR. This can be zero, one,
// or multiple LLVM IR instructions and/or runtime calls.
func (b *builder) createExpr(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpAdd8, ssa.OpAdd16, ssa.OpAdd32, ssa.OpAdd64:
		return b.createBinOp(expr)
	case ssa.OpAddPtr:
		return b.createBinOp(expr)
	case ssa.OpAdd32F, ssa.OpAdd64F:
		return b.createBinOp(expr)
	case ssa.OpSub8, ssa.OpSub16, ssa.OpSub32, ssa.OpSub64:
		return b.createBinOp(expr)
	case ssa.OpSubPtr:
		return b.createBinOp(expr)
	case ssa.OpSub32F, ssa.OpSub64F:
		return b.createBinOp(expr)
	case ssa.OpMul8, ssa.OpMul16, ssa.OpMul32, ssa.OpMul64:
		return b.createBinOp(expr)
	case ssa.OpMul32F, ssa.OpMul64F:
		return b.createBinOp(expr)
	case ssa.OpDiv8, ssa.OpDiv16, ssa.OpDiv32, ssa.OpDiv64:
		return b.createBinOp(expr)
	case ssa.OpDiv8u, ssa.OpDiv16u, ssa.OpDiv32u, ssa.OpDiv64u:
		return b.createBinOp(expr)
	case ssa.OpDiv128u:
		// we only support amd64 now.
		if b.Config.CPU != llvm.CPU_X86_64 {
			panic("TODO")
		}
		// return type is { i128, i128 }
		return b.createIntrinsicCallWithArgs("llvm.go.div128", b.getArgs(expr), b.getLLVMType(expr.Type))
	case ssa.OpDiv32F, ssa.OpDiv64F:
		return b.createBinOp(expr)
	case ssa.OpHmul32, ssa.OpHmul32u, ssa.OpHmul64, ssa.OpHmul64u:
		return b.createBinOp(expr)
	case ssa.OpMul32uhilo, ssa.OpMul64uhilo:
		return b.createBinOp(expr)
	case ssa.OpMul32uover, ssa.OpMul64uover:
		return b.createBinOp(expr)
	case ssa.OpAvg32u, ssa.OpAvg64u:
		return b.createBinOp(expr)
	case ssa.OpMod8, ssa.OpMod16, ssa.OpMod32, ssa.OpMod64:
		return b.createBinOp(expr)
	case ssa.OpMod8u, ssa.OpMod16u, ssa.OpMod32u, ssa.OpMod64u:
		return b.createBinOp(expr)
	case ssa.OpAnd8, ssa.OpAnd16, ssa.OpAnd32, ssa.OpAnd64:
		return b.createBinOp(expr)
	case ssa.OpOr8, ssa.OpOr16, ssa.OpOr32, ssa.OpOr64:
		return b.createBinOp(expr)
	case ssa.OpXor8, ssa.OpXor16, ssa.OpXor32, ssa.OpXor64:
		return b.createBinOp(expr)
	case ssa.OpLsh8x8, ssa.OpLsh8x16, ssa.OpLsh8x32, ssa.OpLsh8x64:
		return b.createBinShift(expr)
	case ssa.OpLsh16x8, ssa.OpLsh16x16, ssa.OpLsh16x32, ssa.OpLsh16x64:
		return b.createBinShift(expr)
	case ssa.OpLsh32x8, ssa.OpLsh32x16, ssa.OpLsh32x32, ssa.OpLsh32x64:
		return b.createBinShift(expr)
	case ssa.OpLsh64x8, ssa.OpLsh64x16, ssa.OpLsh64x32, ssa.OpLsh64x64:
		return b.createBinShift(expr)
	case ssa.OpRsh8x8, ssa.OpRsh8x16, ssa.OpRsh8x32, ssa.OpRsh8x64:
		return b.createBinShift(expr)
	case ssa.OpRsh16x8, ssa.OpRsh16x16, ssa.OpRsh16x32, ssa.OpRsh16x64:
		return b.createBinShift(expr)
	case ssa.OpRsh32x8, ssa.OpRsh32x16, ssa.OpRsh32x32, ssa.OpRsh32x64:
		return b.createBinShift(expr)
	case ssa.OpRsh64x8, ssa.OpRsh64x16, ssa.OpRsh64x32, ssa.OpRsh64x64:
		return b.createBinShift(expr)
	case ssa.OpRsh8Ux8, ssa.OpRsh8Ux16, ssa.OpRsh8Ux32, ssa.OpRsh8Ux64:
		return b.createBinShift(expr)
	case ssa.OpRsh16Ux8, ssa.OpRsh16Ux16, ssa.OpRsh16Ux32, ssa.OpRsh16Ux64:
		return b.createBinShift(expr)
	case ssa.OpRsh32Ux8, ssa.OpRsh32Ux16, ssa.OpRsh32Ux32, ssa.OpRsh32Ux64:
		return b.createBinShift(expr)
	case ssa.OpRsh64Ux8, ssa.OpRsh64Ux16, ssa.OpRsh64Ux32, ssa.OpRsh64Ux64:
		return b.createBinShift(expr)
	case ssa.OpEq8, ssa.OpEq16, ssa.OpEq32, ssa.OpEq64:
		return b.createBinOp(expr)
	case ssa.OpEqPtr:
		return b.createBinOp(expr)
	case ssa.OpEqInter:
		return b.createBinOp(expr)
	case ssa.OpEqSlice:
		return b.createBinOp(expr)
	case ssa.OpEq32F, ssa.OpEq64F:
		return b.createBinOp(expr)
	case ssa.OpNeq8, ssa.OpNeq16, ssa.OpNeq32, ssa.OpNeq64:
		return b.createBinOp(expr)
	case ssa.OpNeqPtr:
		return b.createBinOp(expr)
	case ssa.OpNeqInter:
		return b.createBinOp(expr)
	case ssa.OpNeqSlice:
		return b.createBinOp(expr)
	case ssa.OpNeq32F, ssa.OpNeq64F:
		return b.createBinOp(expr)
	case ssa.OpLess8, ssa.OpLess16, ssa.OpLess32, ssa.OpLess64:
		return b.createBinOp(expr)
	case ssa.OpLess8U, ssa.OpLess16U, ssa.OpLess32U, ssa.OpLess64U:
		return b.createBinOp(expr)
	case ssa.OpLess32F, ssa.OpLess64F:
		return b.createBinOp(expr)
	case ssa.OpLeq8, ssa.OpLeq16, ssa.OpLeq32, ssa.OpLeq64:
		return b.createBinOp(expr)
	case ssa.OpLeq8U, ssa.OpLeq16U, ssa.OpLeq32U, ssa.OpLeq64U:
		return b.createBinOp(expr)
	case ssa.OpLeq32F, ssa.OpLeq64F:
		return b.createBinOp(expr)
	case ssa.OpCondSelect:
		x, y, c := b.getValue(expr.Args[0]), b.getValue(expr.Args[1]), b.getValue(expr.Args[2])
		typ := b.getLLVMType(expr.Type)
		x = b.extendedBitcast(x, typ)
		y = b.extendedBitcast(y, typ)
		return b.CreateSelect(c, x, y, "")
	case ssa.OpAndB, ssa.OpOrB, ssa.OpEqB, ssa.OpNeqB:
		return b.createBinOp(expr)
	case ssa.OpNot:
		return b.createUnOp(expr)
	case ssa.OpNeg8, ssa.OpNeg16, ssa.OpNeg32, ssa.OpNeg64:
		return b.createUnOp(expr)
	case ssa.OpNeg32F, ssa.OpNeg64F:
		return b.createUnOp(expr)
	case ssa.OpCom8, ssa.OpCom16, ssa.OpCom32, ssa.OpCom64:
		return b.createUnOp(expr)
	case ssa.OpCtz8, ssa.OpCtz8NonZero:
		args := b.getArgs(expr)
		args = append(args, llvm.ConstInt(b.int1Type, 0, false))
		val := b.createIntrinsicCallWithArgs("llvm.cttz.i8", args, args[0].Type())
		return b.signExtend(val, b.intType)
	case ssa.OpCtz16, ssa.OpCtz16NonZero:
		args := b.getArgs(expr)
		args = append(args, llvm.ConstInt(b.int1Type, 0, false))
		val := b.createIntrinsicCallWithArgs("llvm.cttz.i16", args, args[0].Type())
		return b.signExtend(val, b.intType)
	case ssa.OpCtz32, ssa.OpCtz32NonZero:
		args := b.getArgs(expr)
		args = append(args, llvm.ConstInt(b.int1Type, 0, false))
		val := b.createIntrinsicCallWithArgs("llvm.cttz.i32", args, args[0].Type())
		return b.signExtend(val, b.intType)
	case ssa.OpCtz64, ssa.OpCtz64NonZero:
		args := b.getArgs(expr)
		args = append(args, llvm.ConstInt(b.int1Type, 0, false))
		val := b.createIntrinsicCallWithArgs("llvm.cttz.i64", args, args[0].Type())
		return b.signExtend(val, b.intType)
	case ssa.OpBitLen8:
		args := b.getArgs(expr)
		args = append(args, llvm.ConstInt(b.int1Type, 0, false))
		val := b.createIntrinsicCallWithArgs("llvm.ctlz.i8", args, b.int8Type)
		return b.CreateSub(llvm.ConstInt(b.intType, 8, false), b.signExtend(val, b.intType), "")
	case ssa.OpBitLen16:
		args := b.getArgs(expr)
		args = append(args, llvm.ConstInt(b.int1Type, 0, false))
		val := b.createIntrinsicCallWithArgs("llvm.ctlz.i16", args, b.int16Type)
		return b.CreateSub(llvm.ConstInt(b.intType, 16, false), b.signExtend(val, b.intType), "")
	case ssa.OpBitLen32:
		args := b.getArgs(expr)
		args = append(args, llvm.ConstInt(b.int1Type, 0, false))
		val := b.createIntrinsicCallWithArgs("llvm.ctlz.i32", args, b.int32Type)
		return b.CreateSub(llvm.ConstInt(b.intType, 32, false), b.signExtend(val, b.intType), "")
	case ssa.OpBitLen64:
		args := b.getArgs(expr)
		args = append(args, llvm.ConstInt(b.int1Type, 0, false))
		val := b.createIntrinsicCallWithArgs("llvm.ctlz.i64", args, b.getLLVMType(expr.Type))
		return b.CreateBinOp(llvm.Sub, llvm.ConstInt(b.intType, 64, false), b.signExtend(val, b.intType), "")
	case ssa.OpBswap16:
		return b.createIntrinsicCallWithArgs("llvm.bswap.i16", b.getArgs(expr), b.int16Type)
	case ssa.OpBswap32:
		return b.createIntrinsicCallWithArgs("llvm.bswap.i32", b.getArgs(expr), b.int32Type)
	case ssa.OpBswap64:
		return b.createIntrinsicCallWithArgs("llvm.bswap.i64", b.getArgs(expr), b.int64Type)
	case ssa.OpBitRev8:
		return b.signExtend(b.createIntrinsicCallWithArgs("llvm.bitreverse.i8", b.getArgs(expr), b.int8Type), b.intType)
	case ssa.OpBitRev16:
		return b.signExtend(b.createIntrinsicCallWithArgs("llvm.bitreverse.i16", b.getArgs(expr), b.int16Type), b.intType)
	case ssa.OpBitRev32:
		return b.signExtend(b.createIntrinsicCallWithArgs("llvm.bitreverse.i32", b.getArgs(expr), b.int32Type), b.intType)
	case ssa.OpBitRev64:
		return b.signExtend(b.createIntrinsicCallWithArgs("llvm.bitreverse.i64", b.getArgs(expr), b.int64Type), b.intType)
	case ssa.OpPopCount8:
		x := b.unsignExtend(b.getValue(expr.Args[0]), b.int8Type)
		return b.signExtend(b.createIntrinsicCallWithArgs("llvm.ctpop.i8", []llvm.Value{x}, b.int8Type), b.intType)
	case ssa.OpPopCount16:
		x := b.unsignExtend(b.getValue(expr.Args[0]), b.int16Type)
		return b.signExtend(b.createIntrinsicCallWithArgs("llvm.ctpop.i16", []llvm.Value{x}, b.int16Type), b.intType)
	case ssa.OpPopCount32:
		x := b.unsignExtend(b.getValue(expr.Args[0]), b.int32Type)
		return b.signExtend(b.createIntrinsicCallWithArgs("llvm.ctpop.i32", []llvm.Value{x}, b.int32Type), b.intType)
	case ssa.OpPopCount64:
		x := b.unsignExtend(b.getValue(expr.Args[0]), b.int64Type)
		return b.signExtend(b.createIntrinsicCallWithArgs("llvm.ctpop.i64", []llvm.Value{x}, b.int64Type), b.intType)
	case ssa.OpRotateLeft8:
		val := b.getValue(expr.Args[0])
		val = b.integerConvert(val, b.int8Type, true)
		shf := b.getValue(expr.Args[1])
		shf = b.integerConvert(shf, b.int8Type, true)
		return b.createIntrinsicCallWithArgs("llvm.fshl.i8", []llvm.Value{val, val, shf}, b.getLLVMType(expr.Type))
	case ssa.OpRotateLeft16:
		val := b.getValue(expr.Args[0])
		val = b.integerConvert(val, b.int16Type, true)
		shf := b.getValue(expr.Args[1])
		shf = b.integerConvert(shf, b.int16Type, true)
		return b.createIntrinsicCallWithArgs("llvm.fshl.i16", []llvm.Value{val, val, shf}, b.getLLVMType(expr.Type))
	case ssa.OpRotateLeft32:
		val := b.getValue(expr.Args[0])
		val = b.integerConvert(val, b.int32Type, true)
		shf := b.getValue(expr.Args[1])
		shf = b.integerConvert(shf, b.int32Type, true)
		return b.createIntrinsicCallWithArgs("llvm.fshl.i32", []llvm.Value{val, val, shf}, b.getLLVMType(expr.Type))
	case ssa.OpRotateLeft64:
		val := b.getValue(expr.Args[0])
		val = b.integerConvert(val, b.int64Type, true)
		shf := b.getValue(expr.Args[1])
		shf = b.integerConvert(shf, b.int64Type, true)
		return b.createIntrinsicCallWithArgs("llvm.fshl.i64", []llvm.Value{val, val, shf}, b.getLLVMType(expr.Type))
	case ssa.OpSqrt:
		return b.createIntrinsicCallWithArgs("llvm.sqrt.f64", b.getArgs(expr), b.f64Type)
	case ssa.OpSqrt32:
		return b.createIntrinsicCallWithArgs("llvm.sqrt.f32", b.getArgs(expr), b.f32Type)
	case ssa.OpFloor:
		return b.createIntrinsicCallWithArgs("llvm.floor.f64", b.getArgs(expr), b.f64Type)
	case ssa.OpCeil:
		return b.createIntrinsicCallWithArgs("llvm.ceil.f64", b.getArgs(expr), b.f64Type)
	case ssa.OpTrunc:
		return b.createIntrinsicCallWithArgs("llvm.trunc.f64", b.getArgs(expr), b.f64Type)
	case ssa.OpRound:
		return b.createIntrinsicCallWithArgs("llvm.round.f64", b.getArgs(expr), b.f64Type)
	case ssa.OpRoundToEven:
		// can't link roundeven on macos
		// so we use function of golang
		sym := getGoFunctionSym("math", "RoundToEven", obj.ABIInternal)
		return b.createGoCall(sym, expr)
	case ssa.OpAbs:
		return b.createIntrinsicCallWithArgs("llvm.fabs.f64", b.getArgs(expr), b.f64Type)
	case ssa.OpCopysign:
		// TODO: the document of math.Copysign states that the first argument is magnitude and
		// the second argument is the sign. However, the comments in genericOps.go is opposite.
		return b.createIntrinsicCallWithArgs("llvm.copysign.f64", b.getArgs(expr), b.f64Type)
	case ssa.OpFMA:
		// we only support amd64 now.
		if b.Config.CPU != llvm.CPU_X86_64 {
			panic("TODO")
		}
		return b.createIntrinsicCallWithArgs("llvm.fma.f64", b.getArgs(expr), b.f64Type)
	case ssa.OpPhi:
		// a merge point of memory
		if expr.Type == types.TypeMem {
			return llvm.Value{}
		}
		// create a PHI placeholder here
		return b.CreatePHI(b.getLLVMType(expr.Type), "")
	// OpRound32F and OpRound64F will be written to OpCopy when lowering.
	case ssa.OpCopy, ssa.OpRound32F, ssa.OpRound64F:
		return b.createCopy(expr)
	case ssa.OpConvert:
		// ptr to int or int to ptr
		from := b.getValue(expr.Args[0])
		fromType := from.Type()
		toType := b.getLLVMType(expr.Type)
		if fromType.TypeKind() == llvm.IntegerTypeKind && toType.TypeKind() == llvm.PointerTypeKind {
			if !from.IsAInstruction().IsNil() {
				mdid := b.ctx.MDKindID("go.pointerlike")
				from.SetMetadata(mdid, b.ctx.MDNode(nil))
			}
		}
		return b.extendedBitcast(from, toType)
	case ssa.OpConstBool:
		return b.createConst(expr)
	case ssa.OpConstString:
		return b.createConst(expr)
	case ssa.OpConstNil:
		return b.createConst(expr)
	case ssa.OpConst8, ssa.OpConst16, ssa.OpConst32, ssa.OpConst64:
		return b.createConst(expr)
	case ssa.OpConst32F, ssa.OpConst64F:
		return b.createConst(expr)
	case ssa.OpConstInterface:
		return b.createConst(expr)
	case ssa.OpConstSlice:
		return b.createConst(expr)
	case ssa.OpInitMem:
		// stmt:
		shouldNotReachHere()
	case ssa.OpArg:
		return b.getArg(expr)
	case ssa.OpArgIntReg, ssa.OpArgFloatReg:
		return b.getArgReg(expr)
	case ssa.OpAddr:
		g := b.getGlobal(expr)
		t := b.getLLVMType(expr.Type)
		switch expr.Type.Kind() {
		case types.TUINTPTR:
			// cast a pointer to uintptr
			g = b.CreatePtrToInt(g, t, "")
		case types.TPTR:
			// In SSA, funcptr and funcclosure has the same func type.
			// The Addr may load a value from a global variable which cannot end with ·f
			et := expr.Type.Elem()
			if et.Kind() == types.TFUNC {
				// TODO: we use symbol to check whether a global is a sym to go.func.*
				s := expr.Aux.(*obj.LSym)
				if strings.HasSuffix(s.Name, "·f") {
					// the llvm type must be a pointer to a pointer to a func struct, i.e., a pointer to funcval in runtime2.go
					if t.TypeKind() != llvm.PointerTypeKind && t.ElementType().TypeKind() != llvm.PointerTypeKind {
						base.FatalfAt(expr.Pos, "Must be a pointer to a pointer to a struct")
					}
					t = g.GlobalValueType()
					g = b.CreateBitCast(g, t, "")
				}
			}
		}
		return g
	case ssa.OpLocalAddr:
		return b.getLocal(expr)
	case ssa.OpSP:
		return b.createSP(expr)
	case ssa.OpSB:
		shouldNotReachHere()
	case ssa.OpInvalid:
		// unused value
		shouldNotReachHere()
	case ssa.OpLoad:
		addr := b.getValue(expr.Args[0])
		t := addr.Type()
		llvmTy := b.getLLVMType(expr.Type)
		vt := llvm.PointerType(llvmTy, 0)
		if t != vt {
			addr = b.toPtr(addr, vt)
		}
		return b.CreateLoad(llvmTy, addr, "")
	case ssa.OpDereference:
		// OpDereference is the same as load and
		// has been rewritten to load in function expandCalls.
		shouldNotReachHere()
	case ssa.OpStore:
		// stmt
		shouldNotReachHere()
	case ssa.OpMove:
		// stmt: call memmove
		shouldNotReachHere()
	case ssa.OpZero:
		// see createInstruction
		shouldNotReachHere()
	case ssa.OpStoreWB, ssa.OpMoveWB, ssa.OpZeroWB:
		// These opcode instructions will be optimized out before the lower pass
		// See writebarrier in src/cmd/compile/internal/ssa/writebarrier.go
	case ssa.OpWB:
		if b.Config.CPU != llvm.CPU_X86_64 {
			panic("TODO: amd64 only")
		}
		// the LLVM backend now can handle the ad-hoc gcWriteBarrier ABI
		n := expr.AuxInt
		if n < 1 || n > 8 {
			base.FatalfAt(expr.Pos, "cannot determine gcWriteBarrier for %v", expr)
		}
		llvmfn := b.getGCWriteBarrierFn(n)
		fntype := b.gcWriteBarrierFnType
		return b.makeCall(fntype, llvmfn, []llvm.Value{}, llvm.GO_ABIINTERNAL_CCONV, "")
	case ssa.OpHasCPUFeature:
		gaddr := b.getGlobal(expr)
		g := b.CreateLoad(gaddr.GlobalValueType(), gaddr, "")
		return b.CreateICmp(llvm.IntEQ, llvm.ConstInt(b.int1Type, 1, false), g, "")
	case ssa.OpPanicBounds:
		shouldNotReachHere()
		// we need to generate a call to runtime.panicindex
	case ssa.OpPanicExtend: // 32bit arch only
		shouldNotReachHere()
	case ssa.OpClosureCall, ssa.OpStaticCall, ssa.OpInterCall, ssa.OpTailCall:
		return b.createFunctionCall(expr)
	case ssa.OpClosureLECall, ssa.OpStaticLECall, ssa.OpInterLECall, ssa.OpTailLECall:
		return b.createFunctionCall(expr)
	case ssa.OpSignExt8to16, ssa.OpSignExt8to32, ssa.OpSignExt8to64, ssa.OpSignExt16to32, ssa.OpSignExt16to64,
		ssa.OpSignExt32to64:
		// i1 to i8
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateSExt(val, t, "")
	case ssa.OpZeroExt8to16, ssa.OpZeroExt8to32, ssa.OpZeroExt8to64, ssa.OpZeroExt16to32, ssa.OpZeroExt16to64,
		ssa.OpZeroExt32to64:
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateZExt(val, t, "")
	case ssa.OpTrunc16to8, ssa.OpTrunc32to8, ssa.OpTrunc32to16, ssa.OpTrunc64to8, ssa.OpTrunc64to16, ssa.OpTrunc64to32:
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateTrunc(val, t, "")
	case ssa.OpCvt32to32F, ssa.OpCvt32to64F, ssa.OpCvt64to32F, ssa.OpCvt64to64F:
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateSIToFP(val, t, "")
	case ssa.OpCvt32Fto32, ssa.OpCvt32Fto64, ssa.OpCvt64Fto32, ssa.OpCvt64Fto64:
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateFPToSI(val, t, "")
	case ssa.OpCvt32Fto64F:
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateFPExt(val, t, "")
	case ssa.OpCvt64Fto32F:
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateFPTrunc(val, t, "")
	case ssa.OpCvtBoolToUint8:
		// i1 to i8
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateZExt(val, t, "")
	case ssa.OpIsNonNil:
		// nil constant, 0 for integer and nil for pointer
		x := b.getValue(expr.Args[0])
		return b.CreateICmp(llvm.IntNE, x, llvm.ConstNull(x.Type()), "")
	case ssa.OpIsInBounds:
		x := b.getValue(expr.Args[0])
		y := b.getValue(expr.Args[1])
		return b.CreateICmp(llvm.IntULT, x, y, "")
	case ssa.OpIsSliceInBounds:
		x := b.getValue(expr.Args[0])
		y := b.getValue(expr.Args[1])
		return b.CreateICmp(llvm.IntULE, x, y, "")
	case ssa.OpNilCheck:
		// mimics amd64/ssa.go:
		// "ssa.OpAMD64LoweredNilCheck: Issue a load which will fault if the input is nil."
		ptr := b.getValue(expr.Args[0])
		ptr = b.toPtr(ptr, b.i8ptrType)
		l := b.CreateLoad(b.int8Type, ptr, "")
		// need to set volatile so this dead load will be not be optimized away
		l.SetVolatile(true)
		l.SetMetadata(b.ctx.MDKindID("go.nilcheckload"), b.ctx.MDNode([]llvm.Metadata{}))
		return ptr
	case ssa.OpGetG:
		// TODO: This only supports AMD64, other architecture need to do.
		// TODO: create a LLVM intrinsic for getg.
		if expr.Block.Func.OwnAux.Fn.ABI() == obj.ABIInternal {
			r14 := b.ctx.MDNode([]llvm.Metadata{b.ctx.MDString("r14")})
			call := b.createIntrinsicCallWithArgs("llvm.read_register.i64", []llvm.Value{llvm.MetadataAsValue(b.ctx, r14)}, b.int64Type)
			result := b.toPtr(call, b.i8ptrType)
			return result
		}
		// TODO: REG_TLS
	case ssa.OpGetClosurePtr:
		rdx := b.ctx.MDNode([]llvm.Metadata{b.ctx.MDString("rdx")})
		b.inlinable = false
		return b.createIntrinsicCallWithArgs("llvm.read_register.i64", []llvm.Value{llvm.MetadataAsValue(b.ctx, rdx)}, b.int64Type)
	case ssa.OpGetCallerPC:
		b.inlinable = false
		// 0 means callee, 1 means caller.
		rbp := b.createIntrinsicCallWithArgs("llvm.frameaddress.p0", []llvm.Value{llvm.ConstInt(b.int32Type, 0, false)}, b.i64ptrType)
		// callerpc is stored in address +8 behind rbp on amd64.
		addr := b.createGEPFromFrontend(b.int64Type, rbp, []llvm.Value{llvm.ConstInt(b.int64Type, 1, false)}, "")
		return b.CreateLoad(b.int64Type, addr, "callerpc")
	case ssa.OpGetCallerSP:
		b.inlinable = false
		// 0 means callee, 1 means caller.
		rbp := b.createIntrinsicCallWithArgs("llvm.frameaddress.p0", []llvm.Value{llvm.ConstInt(b.int32Type, 0, false)}, b.i64ptrType)
		// callersp is +16 behind rbp on amd64.
		return b.createGEPFromFrontend(b.int64Type, rbp, []llvm.Value{llvm.ConstInt(b.int64Type, 2, false)}, "callersp")
	case ssa.OpPtrIndex:
		// getelementptr
		ptr := b.getValue(expr.Args[0])
		idx := b.getValue(expr.Args[1])
		return b.createGEPFromFrontend(b.int8Type, ptr, []llvm.Value{idx}, "")
	case ssa.OpOffPtr:
		baseptr := expr.Args[0]
		idx := expr.AuxInt // offset in byte
		if idx == 0 {
			return b.getValue(baseptr)
		}
		ptr := b.getValue(baseptr)
		if baseptr.Type.IsPtr() && ptr.Type().TypeKind() == llvm.PointerTypeKind {
			gep := b.CreateGEPByOffset(ptr, b.getLLVMType(baseptr.Type.Elem()), uint64(idx))
			if !gep.IsNil() {
				if !gep.IsAInstruction().IsNil() {
					gep.SetMetadata(b.ctx.MDKindID("go.gep"), b.ctx.MDNode(nil))
				}
				return gep
			}
		}
		// not a pointer or getGEPIndicesForOffset fails to compute indices
		// 1. get index value
		idxValue := llvm.ConstInt(b.int64Type, uint64(idx), false)
		// 2. cast base ptr to i8*
		ptr = b.toPtr(ptr, b.i8ptrType)
		// 3. get element as i8*
		ptr = b.createGEPFromFrontend(b.int8Type, ptr, []llvm.Value{idxValue}, "")
		return ptr
	case ssa.OpSliceMake, ssa.OpSlicePtr, ssa.OpSliceLen, ssa.OpSliceCap, ssa.OpSlicePtrUnchecked:
		return b.createSliceOp(expr)
	case ssa.OpComplexMake, ssa.OpComplexReal, ssa.OpComplexImag:
		return b.createComplexOp(expr)
	case ssa.OpStringMake, ssa.OpStringPtr, ssa.OpStringLen:
		return b.createStringOp(expr)
	case ssa.OpIMake, ssa.OpITab, ssa.OpIData:
		return b.createIfaceOp(expr)
	case ssa.OpStructMake, ssa.OpStructSelect:
		return b.createStructOp(expr)
	case ssa.OpEmpty:
		return llvm.Undef(b.getLLVMType(expr.Type))
	case ssa.OpArrayMake1, ssa.OpArraySelect:
		return b.createArrayOp(expr)
	case ssa.OpStoreReg:
		// see regalloc
		shouldNotReachHere()
	case ssa.OpLoadReg:
		// see regalloc
		shouldNotReachHere()
	case ssa.OpFwdRef:
		shouldNotReachHere()
	case ssa.OpUnknown:
		return llvm.Value{}
	case ssa.OpVarDef, ssa.OpVarLive, ssa.OpKeepAlive:
		// nop
		shouldNotReachHere()
	case ssa.OpInlMark:
		shouldNotReachHere()
	case ssa.OpInt64Make, ssa.OpInt64Hi, ssa.OpInt64Lo:
		// 32-bit
		shouldNotReachHere()
	case ssa.OpAdd32carry:
		// 32-bit
		shouldNotReachHere()
	case ssa.OpAdd32withcarry:
		// 32-bit
		shouldNotReachHere()
	case ssa.OpSub32carry:
		// 32-bit
		shouldNotReachHere()
	case ssa.OpSub32withcarry:
		// 32-bit
		shouldNotReachHere()
	case ssa.OpAdd64carry:
		// arg0 + arg1 + arg2, arg2 must be 0 or 1. returns (value, value>>64)
		tuint64 := types.Types[types.TUINT64]
		tbool := types.Types[types.TBOOL]
		retType := b.getLLVMType(types.NewResults([]*types.Type{tuint64, tbool, types.TypeMem}))
		arg0 := b.getValue(expr.Args[0])
		arg1 := b.getValue(expr.Args[1])
		arg2 := b.getValue(expr.Args[2])
		ret1 := b.createIntrinsicCallWithArgs("llvm.uadd.with.overflow.i64", []llvm.Value{arg0, arg1}, retType)
		arg0 = b.CreateExtractValue(ret1, 0, "")
		ret2 := b.createIntrinsicCallWithArgs("llvm.uadd.with.overflow.i64", []llvm.Value{arg0, arg2}, retType)
		carry1 := b.CreateExtractValue(ret1, 1, "")
		carry2 := b.CreateExtractValue(ret2, 1, "")
		carry := b.CreateZExt(b.CreateBinOp(llvm.Or, carry1, carry2, ""), b.int64Type, "")
		ret := b.CreateExtractValue(ret2, 0, "")
		retType = b.getLLVMType(expr.Type)
		val := llvm.Undef(retType)
		val = b.CreateInsertValue(val, ret, 0, "")
		val = b.CreateInsertValue(val, carry, 1, "")
		return val
	case ssa.OpSub64borrow:
		// arg0 - (arg1 + arg2), arg2 must be 0 or 1. returns (value, value>>64&1)
		tuint64 := types.Types[types.TUINT64]
		tbool := types.Types[types.TBOOL]
		retType := b.getLLVMType(types.NewResults([]*types.Type{tuint64, tbool, types.TypeMem}))
		arg0 := b.getValue(expr.Args[0])
		arg1 := b.getValue(expr.Args[1])
		arg2 := b.getValue(expr.Args[2])
		ret1 := b.createIntrinsicCallWithArgs("llvm.usub.with.overflow.i64", []llvm.Value{arg0, arg1}, retType)
		arg0 = b.CreateExtractValue(ret1, 0, "")
		ret2 := b.createIntrinsicCallWithArgs("llvm.usub.with.overflow.i64", []llvm.Value{arg0, arg2}, retType)
		carry1 := b.CreateExtractValue(ret1, 1, "")
		carry2 := b.CreateExtractValue(ret2, 1, "")
		carry := b.CreateZExt(b.CreateBinOp(llvm.Or, carry1, carry2, ""), b.int64Type, "")
		ret := b.CreateExtractValue(ret2, 0, "")
		retType = b.getLLVMType(expr.Type)
		val := llvm.Undef(retType)
		val = b.CreateInsertValue(val, ret, 0, "")
		val = b.CreateInsertValue(val, carry, 1, "")
		return val
	case ssa.OpSignmask:
		// 0 if arg0 >= 0, -1 if arg0 < 0
		x := b.getValue(expr.Args[0])
		y := llvm.ConstInt(x.Type(), 0, false)
		z := llvm.ConstInt(b.intType, 0, false)
		m := llvm.ConstInt(b.intType, ^uint64(0), false)
		var c llvm.Value
		if expr.Args[0].Type.IsSigned() {
			c = b.CreateICmp(llvm.IntSGE, x, y, "")
		} else {
			c = b.CreateICmp(llvm.IntUGE, x, y, "")
		}
		return b.CreateSelect(c, z, m, "")
	case ssa.OpZeromask:
		// 0 if arg0 == 0, 0xffffffff if arg0 != 0
		x := b.getValue(expr.Args[0])
		y := llvm.ConstInt(x.Type(), 0, false)
		z := llvm.ConstInt(b.intType, 0, false)
		m := llvm.ConstInt(b.intType, 0xffffffff, false)
		c := b.CreateICmp(llvm.IntEQ, x, y, "")
		return b.CreateSelect(c, z, m, "")
	case ssa.OpSlicemask:
		// 0 if arg0 == 0, -1 if arg0 > 0, undef if arg0<0. Type is native int size.
		x := b.getValue(expr.Args[0])
		c63 := llvm.ConstInt(b.intType, uint64(b.intType.IntTypeWidth()-1), false)
		return b.CreateAShr(b.CreateNeg(x, ""), c63, "")
	case ssa.OpSpectreIndex:
		// arg0 if 0 <= arg0 < arg1, 0 otherwise. Type is native int size.
		x := b.getValue(expr.Args[0])
		y := b.getValue(expr.Args[1])
		z := llvm.ConstInt(x.Type(), 0, false)
		r := llvm.ConstInt(b.intType, 0, false)
		intx := b.integerConvert(x, b.intType, true)
		var c1 llvm.Value
		var c2 llvm.Value
		if expr.Args[0].Type.IsSigned() {
			c1 = b.CreateICmp(llvm.IntSLT, x, z, "")
			c2 = b.CreateICmp(llvm.IntSGE, x, y, "")
		} else {
			c1 = b.CreateICmp(llvm.IntULT, x, z, "")
			c2 = b.CreateICmp(llvm.IntSGE, x, y, "")
		}
		return b.CreateSelect(c1, r, b.CreateSelect(c2, r, intx, ""), "")
	case ssa.OpSpectreSliceIndex:
		// arg0 if 0 <= arg0 <= arg1, 0 otherwise. Type is native int size.
		x := b.getValue(expr.Args[0])
		y := b.getValue(expr.Args[1])
		z := llvm.ConstInt(x.Type(), 0, false)
		r := llvm.ConstInt(b.intType, 0, false)
		intx := b.integerConvert(x, b.intType, true)
		var c1 llvm.Value
		var c2 llvm.Value
		if expr.Args[0].Type.IsSigned() {
			c1 = b.CreateICmp(llvm.IntSLT, x, z, "")
			c2 = b.CreateICmp(llvm.IntSGT, x, y, "")
		} else {
			c1 = b.CreateICmp(llvm.IntULT, x, z, "")
			c2 = b.CreateICmp(llvm.IntSGT, x, y, "")
		}
		return b.CreateSelect(c1, r, b.CreateSelect(c2, r, intx, ""), "")
	case ssa.OpCvt32Uto32F, ssa.OpCvt32Uto64F, ssa.OpCvt64Uto32F, ssa.OpCvt64Uto64F:
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateUIToFP(val, t, "")
	case ssa.OpCvt32Fto32U, ssa.OpCvt64Fto32U, ssa.OpCvt32Fto64U, ssa.OpCvt64Fto64U:
		val := b.getValue(expr.Args[0])
		t := b.getLLVMType(expr.Type)
		return b.CreateFPToUI(val, t, "")
	case ssa.OpSelect0, ssa.OpSelect1, ssa.OpSelectN, ssa.OpSelectNAddr, ssa.OpMakeResult:
		return b.createResultOp(expr) // almost the same as struct op
	case ssa.OpAtomicLoad8, ssa.OpAtomicLoad32, ssa.OpAtomicLoad64, ssa.OpAtomicLoadPtr:
		ptr := b.getValue(expr.Args[0])
		valType := b.getLLVMType(expr.Type)
		ptr = b.toPtr(ptr, llvm.PointerType(valType, 0))
		val := b.CreateLoad(valType, ptr, "")
		val.SetOrdering(llvm.AtomicOrderingSequentiallyConsistent)
		val.SetAlignment(b.targetData.PrefTypeAlignment(val.Type())) // required
		return val
	case ssa.OpAtomicLoadAcq32, ssa.OpAtomicLoadAcq64:
		ptr := b.getValue(expr.Args[0])
		valType := b.getLLVMType(expr.Type)
		ptr = b.toPtr(ptr, llvm.PointerType(valType, 0))
		val := b.CreateLoad(valType, ptr, "")
		val.SetOrdering(llvm.AtomicOrderingAcquire)
		val.SetAlignment(b.targetData.PrefTypeAlignment(val.Type())) // required
		return val
	case ssa.OpAtomicStore8, ssa.OpAtomicStore32, ssa.OpAtomicStore64, ssa.OpAtomicStorePtrNoWB:
		ptr := b.getValue(expr.Args[0])
		val := b.getValue(expr.Args[1])
		ptr = b.toPtr(ptr, llvm.PointerType(val.Type(), 0))
		store := b.CreateStore(val, ptr)
		store.SetOrdering(llvm.AtomicOrderingSequentiallyConsistent)
		store.SetAlignment(b.targetData.PrefTypeAlignment(val.Type())) // required
		return llvm.Value{}                                            // should be stmt not expr
	case ssa.OpAtomicStoreRel32, ssa.OpAtomicStoreRel64:
		ptr := b.getValue(expr.Args[0])
		val := b.getValue(expr.Args[1])
		ptr = b.toPtr(ptr, llvm.PointerType(val.Type(), 0))
		store := b.CreateStore(val, ptr)
		store.SetOrdering(llvm.AtomicOrderingRelease)
		store.SetAlignment(b.targetData.PrefTypeAlignment(val.Type())) // required
		return llvm.Value{}
	case ssa.OpAtomicExchange8, ssa.OpAtomicExchange32, ssa.OpAtomicExchange64:
		ptr := b.getValue(expr.Args[0])
		val := b.getValue(expr.Args[1])
		ptr = b.toPtr(ptr, llvm.PointerType(val.Type(), 0))
		ret := b.CreateAtomicRMW(llvm.AtomicRMWBinOpXchg, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
		return ret
	case ssa.OpAtomicAdd32, ssa.OpAtomicAdd64:
		ptr := b.getValue(expr.Args[0])
		val := b.getValue(expr.Args[1])
		ptr = b.toPtr(ptr, llvm.PointerType(val.Type(), 0))
		oldVal := b.CreateAtomicRMW(llvm.AtomicRMWBinOpAdd, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
		// Return the new value, not the original value returned by atomicrmw.
		val = b.CreateAdd(oldVal, val, "")
		return val
	case ssa.OpAtomicCompareAndSwap32, ssa.OpAtomicCompareAndSwap64:
		ptr := b.getValue(expr.Args[0])
		old := b.getValue(expr.Args[1])
		newVal := b.getValue(expr.Args[2])
		ptr = b.toPtr(ptr, llvm.PointerType(old.Type(), 0))
		tuple := b.CreateAtomicCmpXchg(ptr, old, newVal, llvm.AtomicOrderingSequentiallyConsistent, llvm.AtomicOrderingSequentiallyConsistent, true)
		swapped := b.CreateExtractValue(tuple, 1, "")
		return swapped
	case ssa.OpAtomicCompareAndSwapRel32:
		ptr := b.getValue(expr.Args[0])
		old := b.getValue(expr.Args[1])
		newVal := b.getValue(expr.Args[2])
		ptr = b.toPtr(ptr, llvm.PointerType(old.Type(), 0))
		tuple := b.CreateAtomicCmpXchg(ptr, old, newVal, llvm.AtomicOrderingRelease, llvm.AtomicOrderingRelease, true)
		swapped := b.CreateExtractValue(tuple, 1, "")
		return swapped
	case ssa.OpAtomicAnd8, ssa.OpAtomicAnd32:
		ptr := b.getValue(expr.Args[0])
		val := b.getValue(expr.Args[1])
		ptr = b.toPtr(ptr, llvm.PointerType(val.Type(), 0))
		// see signature of runtime.And8，OpAtomicAnd8 has no return
		// see signature of runtime.And，OpAtomicAnd32 has no return
		b.CreateAtomicRMW(llvm.AtomicRMWBinOpAnd, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
		return llvm.Value{}
	case ssa.OpAtomicOr8, ssa.OpAtomicOr32:
		ptr := b.getValue(expr.Args[0])
		val := b.getValue(expr.Args[1])
		ptr = b.toPtr(ptr, llvm.PointerType(val.Type(), 0))
		// see signature of runtime.Or8，OpAtomicOr8 has no return
		// see signature of runtime.Or，OpAtomicOr32 has no return
		b.CreateAtomicRMW(llvm.AtomicRMWBinOpOr, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
		return llvm.Value{}
	case ssa.OpAtomicAnd8value, ssa.OpAtomicAnd32value, ssa.OpAtomicAnd64value:
		ptr := b.getValue(expr.Args[0])
		val := b.getValue(expr.Args[1])
		ptr = b.toPtr(ptr, llvm.PointerType(val.Type(), 0))
		return b.CreateAtomicRMW(llvm.AtomicRMWBinOpAnd, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
	case ssa.OpAtomicOr8value, ssa.OpAtomicOr32value, ssa.OpAtomicOr64value:
		ptr := b.getValue(expr.Args[0])
		val := b.getValue(expr.Args[1])
		ptr = b.toPtr(ptr, llvm.PointerType(val.Type(), 0))
		return b.CreateAtomicRMW(llvm.AtomicRMWBinOpOr, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
	case ssa.OpMax64, ssa.OpMin64, ssa.OpMax64u, ssa.OpMin64u:
		cmp := map[ssa.Op]llvm.IntPredicate{
			ssa.OpMax64:  llvm.IntSGE,
			ssa.OpMin64:  llvm.IntSLE,
			ssa.OpMax64u: llvm.IntUGE,
			ssa.OpMin64u: llvm.IntULE,
		}
		x := b.getValue(expr.Args[0])
		y := b.getValue(expr.Args[1])
		return b.CreateSelect(b.CreateICmp(cmp[expr.Op], x, y, ""), x, y, "")
	case ssa.OpMax32F, ssa.OpMax64F, ssa.OpMin32F, ssa.OpMin64F:
		intri := map[ssa.Op]string{
			ssa.OpMax32F: "llvm.maximum.f32",
			ssa.OpMax64F: "llvm.maximum.f64",
			ssa.OpMin32F: "llvm.minimum.f32",
			ssa.OpMin64F: "llvm.minimum.f64",
		}
		return b.createIntrinsicCallWithArgs(intri[expr.Op], b.getArgs(expr), b.getLLVMType(expr.Type))
	case ssa.OpAtomicAdd32Variant:
		shouldNotReachHere()
	case ssa.OpAtomicAdd64Variant:
		shouldNotReachHere()
	case ssa.OpAtomicExchange32Variant:
		shouldNotReachHere()
	case ssa.OpAtomicExchange64Variant:
		shouldNotReachHere()
	case ssa.OpAtomicCompareAndSwap32Variant:
		shouldNotReachHere()
	case ssa.OpAtomicCompareAndSwap64Variant:
		shouldNotReachHere()
	case ssa.OpAtomicAnd8valueVariant:
		shouldNotReachHere()
	case ssa.OpAtomicAnd32valueVariant:
		shouldNotReachHere()
	case ssa.OpAtomicAnd64valueVariant:
		shouldNotReachHere()
	case ssa.OpAtomicOr8valueVariant:
		shouldNotReachHere()
	case ssa.OpAtomicOr32valueVariant:
		shouldNotReachHere()
	case ssa.OpAtomicOr64valueVariant:
		shouldNotReachHere()
	case ssa.OpPubBarrier:
		// x86 doesn't need it, but arm need.
		// We only support x86 now, so we do nothing here.
	case ssa.OpClobber:
		// touch memory at symbol
		// in genssa when computing liveness info
		// which means no such Op before lower
		shouldNotReachHere()
	case ssa.OpClobberReg:
		// only be used in regalloc phase (which means no such Op before lower)
		shouldNotReachHere()
	case ssa.OpPrefetchCache:
		shouldNotReachHere()
	case ssa.OpPrefetchCacheStreamed:
		shouldNotReachHere()
	}

	base.FatalfAt(expr.Pos, "unsupported op: %v", expr.LongString())
	return llvm.Value{}
}

// TODO: remove this
func getGoFunctionSym(pkg string, fn string, abi obj.ABI) *obj.LSym {
	sym := base.Ctxt.Lookup(fmt.Sprintf("%s.%s", pkg, fn))
	sym.Pkg = pkg
	sym.SetABI(abi)
	return sym
}

func shouldNotReachHere() {
	panic("Should Not Reach Here")
}

func (b *builder) createComplexOp(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpComplexMake:
		t := b.getLLVMType(expr.Type)
		r := b.getValue(expr.Args[0]) // real
		i := b.getValue(expr.Args[1]) // image
		val := llvm.Undef(t)
		val = b.CreateInsertValue(val, r, 0, "")
		val = b.CreateInsertValue(val, i, 1, "")
		return val
	case ssa.OpComplexReal:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 0, "")
	case ssa.OpComplexImag:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 1, "")
	default:
		base.FatalfAt(expr.Pos, "Bad complex op: %s", expr.LongString())
	}
	return llvm.Value{}
}

func (b *builder) createIfaceOp(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpIMake:
		t := b.getLLVMType(expr.Type)
		itab := b.getValue(expr.Args[0])
		data := b.toPtr(b.getValue(expr.Args[1]), b.i8ptrType) // data
		val := llvm.Undef(t)
		val = b.CreateInsertValue(val, itab, 0, "")
		val = b.CreateInsertValue(val, data, 1, "")
		return val
	case ssa.OpITab:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 0, "")
	case ssa.OpIData:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 1, "")
	default:
		base.FatalfAt(expr.Pos, "Bad iface op: %s", expr.LongString())
	}
	return llvm.Value{}
}

func (b *builder) createSliceOp(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpSliceMake:
		t := b.getLLVMType(expr.Type)
		p := b.getValue(expr.Args[0]) // ptr
		l := b.getValue(expr.Args[1]) // len
		c := b.getValue(expr.Args[2]) // cap
		val := llvm.Undef(t)
		val = b.CreateInsertValue(val, p, 0, "")
		val = b.CreateInsertValue(val, l, 1, "")
		val = b.CreateInsertValue(val, c, 2, "")
		return val
	case ssa.OpSlicePtr:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 0, "")
	case ssa.OpSliceLen:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 1, "")
	case ssa.OpSliceCap:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 2, "")
	case ssa.OpSlicePtrUnchecked:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 0, "")
	default:
		base.FatalfAt(expr.Pos, "Bad slice op: %s", expr.LongString())
	}
	return llvm.Value{}
}

func (b *builder) createStringOp(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpStringMake:
		t := b.getLLVMType(expr.Type)
		p := b.getValue(expr.Args[0]) // ptr
		l := b.getValue(expr.Args[1]) // len
		val := llvm.Undef(t)
		val = b.CreateInsertValue(val, p, 0, "")
		val = b.CreateInsertValue(val, l, 1, "")
		return val
	case ssa.OpStringPtr:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 0, "")
	case ssa.OpStringLen:
		val := b.getValue(expr.Args[0])
		return b.CreateExtractValue(val, 1, "")
	default:
		base.FatalfAt(expr.Pos, "Bad string op: %s", expr.LongString())
	}
	return llvm.Value{}
}

func (b *builder) createArrayOp(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpArrayMake1:
		// OpArrayMake1 will be rewritten to arg0 in rewritegeneric and expand_calls.
		// shouldNotReachHere()
		t := b.getLLVMType(expr.Type)
		val := llvm.Undef(t)
		x := b.getValue(expr.Args[0])
		val = b.CreateInsertValue(val, x, 0, "")
		return val
	case ssa.OpArraySelect:
		// only array whose length is less than 2 can generate this op,
		// but we know this array will be optimized in rewritegeneric and expand_calls.
		// shouldNotReachHere()
		val := b.getValue(expr.Args[0])
		idx := expr.AuxInt
		return b.CreateExtractValue(val, int(idx), "")
	default:
		base.FatalfAt(expr.Pos, "Bad array op: %s", expr.LongString())
	}
	return llvm.Value{}
}

func (b *builder) createStructOp(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpStructMake:
		t := b.getLLVMType(expr.Type)
		val := llvm.Undef(t)
		for i, a := range expr.Args {
			x := b.getValue(a)
			val = b.CreateInsertValue(val, x, i, "")
		}
		return val
	case ssa.OpStructSelect:
		val := b.getValue(expr.Args[0])
		idx := expr.AuxInt
		return b.CreateExtractValue(val, int(idx), "")
	default:
		base.FatalfAt(expr.Pos, "Bad struct op: %s", expr.LongString())
	}
	return llvm.Value{}
}

func (b *builder) makeResult(llvmType llvm.Type, result []llvm.Value) llvm.Value {
	cur := b.curfn()
	// special case for runtime.deferproc / runtime.deferprocstack
	// create return0 for these cases.
	if cur.LSym == ir.Syms.DeferprocStack || cur.LSym == ir.Syms.Deferproc {
		return llvm.ConstInt(b.int1Type, 0, false)
	}
	val := llvm.Undef(llvmType)
	switch len(result) { // remove trailing mem
	case 0:
		return val
	case 1:
		if llvmType.TypeKind() == llvm.PointerTypeKind {
			result[0] = b.toPtr(result[0], llvmType)
			// result[0] = b.patchValueType(result[0], llvmType)
		}
		return result[0]
	default:
		for i := 0; i < len(result); i++ {
			x := result[i]
			if llvmType.StructElementTypes()[i].TypeKind() == llvm.PointerTypeKind {
				// uintptr should be converted to ptr
				x = b.toPtr(x, llvmType.StructElementTypes()[i])
				// x = b.patchValueType(x, llvmType.StructElementTypes()[i])
			}
			val = b.CreateInsertValue(val, x, i, "")
		}
		// TODO: unwrap empty struct into void
		return val
	}
}

func (b *builder) createResultOp(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpMakeResult:
		// ignore the last mem arg
		result := make([]llvm.Value, len(expr.Args)-1)
		for i := 0; i < len(expr.Args)-1; i++ {
			result[i] = b.getValue(expr.Args[i])
		}
		re := b.makeResult(b.llvmFn.FunctionType().ReturnType(), result)
		return re
	case ssa.OpSelect0:
		if expr.Type.IsMemory() {
			return llvm.Value{}
		}
		val := b.getValue(expr.Args[0])
		valGoType := expr.Args[0].Type
		switch valGoType.Kind() {
		case types.TTUPLE:
			if val.Type().TypeKind() == llvm.StructTypeKind {
				return b.CreateExtractValue(val, 0, "")
			}
			return val // second must be TMEM
		case types.TRESULTS:
			if expr.Args[0].Type.NumFields() == 2 {
				// 0, element; 1, mem
				// unwrapped single element result
				return val
			} else {
				return b.CreateExtractValue(val, 0, "")
			}
		default:
			base.FatalfAt(expr.Pos, "TODO: unknown type of OpSelectX: %v", expr)
		}
	case ssa.OpSelect1:
		if expr.Type.IsMemory() {
			return llvm.Value{}
		}
		val := b.getValue(expr.Args[0])
		valGoType := expr.Args[0].Type
		switch valGoType.Kind() {
		case types.TTUPLE:
			if val.Type().TypeKind() == llvm.StructTypeKind {
				return b.CreateExtractValue(val, 1, "")
			}
			// see makeLLVMType: we assume mem can only be the second of a tuple
			// if reach here, the second must be TMEM
			shouldNotReachHere()
			return llvm.Value{}
		case types.TRESULTS:
			// if get here, expr.Args[0].Type.NumFields() > 2
			return b.CreateExtractValue(val, 1, "")
		default:
			base.FatalfAt(expr.Pos, "TODO: unknown type of OpSelectX: %v", expr)
		}
	case ssa.OpSelectN:
		if expr.Type.IsMemory() {
			return llvm.Value{}
		}
		val := b.getValue(expr.Args[0])
		valGoType := expr.Args[0].Type
		switch valGoType.Kind() {
		case types.TTUPLE:
			_ = b.getLLVMType(valGoType) // force type validation
			// my understanding is that the second element of TUPLE is always mem
			base.FatalfAt(expr.Pos, "TODO: tuple does not use OpSelectN? %v", expr)
		case types.TRESULTS:
			idx := expr.AuxInt
			if idx == 0 {
				if expr.Args[0].Type.NumFields() == 2 {
					// 0, element; 1, mem
					// unwrapped single element result
					return val
				}
			}
			return b.CreateExtractValue(val, int(idx), "")
		default:
			base.FatalfAt(expr.Pos, "TODO: unknown type of OpSelectX: %v", expr)
		}
	case ssa.OpSelectNAddr:
		if expr.Type.IsMemory() {
			return llvm.Value{}
		}
		val := b.getValue(expr.Args[0])
		valGoType := expr.Args[0].Type
		switch valGoType.Kind() {
		case types.TTUPLE:
			_ = b.getLLVMType(valGoType) // force type validation
			// my understanding is that the second element of TUPLE is always mem
			base.FatalfAt(expr.Pos, "TODO: tuple does not use OpSelectN? %v", expr)
		case types.TRESULTS:
			idx := expr.AuxInt
			if idx == 0 {
				if expr.Args[0].Type.NumFields() == 2 {
					// 0, element; 1, mem
					// unwrapped single element result
					return b.createGEPFromFrontend(b.int8Type, val, []llvm.Value{
						llvm.ConstInt(b.intType, 0, false),
					}, "")
				}
			}
			// val is a pointer to results (same as array of results), not a value.
			// so we should get results first.
			// we can CreateLoad and then CreateGEP or
			// we set idx to {0, AuxInt}
			return b.createGEPFromFrontend(b.int8Type, val, []llvm.Value{
				llvm.ConstInt(b.intType, 0, false),
				llvm.ConstInt(b.intType, uint64(expr.AuxInt), false),
			}, "")
		default:
			base.FatalfAt(expr.Pos, "TODO: unknown type of OpSelectX: %v", expr)
		}
	default:
		base.FatalfAt(expr.Pos, "bad result op: %s", expr.LongString())
	}
	return llvm.Value{}
}

func (b *builder) toPtr(val llvm.Value, t llvm.Type) llvm.Value {
	if val.Type() == t {
		return val
	}
	if t.TypeKind() != llvm.PointerTypeKind {
		b.Fatalf("expected a pointer type, got %s", t.String())
	}
	if val.Type().TypeKind() == llvm.PointerTypeKind {
		return b.CreateBitCast(val, t, "")
	}
	if val.Type().TypeKind() == llvm.IntegerTypeKind {
		if val.Type().IntTypeWidth() != b.intType.IntTypeWidth() {
			b.Fatalf("cannot cast %v to pointer", val.Type().String())
		}
		return b.CreateIntToPtr(val, t, "")
	}
	shouldNotReachHere()
	return llvm.Value{}
}

func (b *builder) integerConvert(val llvm.Value, t llvm.Type, signed bool) llvm.Value {
	if val.Type() == t {
		return val
	}
	if val.Type().TypeKind() != llvm.IntegerTypeKind {
		panic("must be integer")
	}
	if t.TypeKind() != llvm.IntegerTypeKind {
		panic("must be integer")
	}
	if val.Type().IntTypeWidth() < t.IntTypeWidth() {
		// do signed extend
		if signed {
			return b.CreateSExt(val, t, "")
		} else {
			return b.CreateZExt(val, t, "")
		}
	} else {
		// trunc
		return b.CreateTrunc(val, t, "")
	}
}

func (b *builder) createBinShift(expr *ssa.Value) llvm.Value {
	x, y := b.getValue(expr.Args[0]), b.getValue(expr.Args[1])
	xt := x.Type()
	yt := y.Type()
	maxbits := llvm.ConstInt(yt, uint64(x.Type().IntTypeWidth())-1, false)
	signed := false
	// Note that maxbits is IntTypeWidth - 1
	geop := llvm.IntUGT
	if expr.Args[1].Type.IsSigned() {
		signed = true
		geop = llvm.IntSGT
	}
	cmp := b.CreateICmp(geop, y, maxbits, "")
	switch expr.Op {
	case ssa.OpLsh8x8, ssa.OpLsh8x16, ssa.OpLsh8x32, ssa.OpLsh8x64,
		ssa.OpLsh16x8, ssa.OpLsh16x16, ssa.OpLsh16x32, ssa.OpLsh16x64,
		ssa.OpLsh32x8, ssa.OpLsh32x16, ssa.OpLsh32x32, ssa.OpLsh32x64,
		ssa.OpLsh64x8, ssa.OpLsh64x16, ssa.OpLsh64x32, ssa.OpLsh64x64:
		if yt.TypeKind() != llvm.IntegerTypeKind {
			panic("must be integer")
		}
		if xt.TypeKind() != llvm.IntegerTypeKind {
			panic("must be integer")
		}
		// TODO: y must not be negative
		shl := b.CreateShl(x, b.integerConvert(y, xt, signed), "")
		return b.CreateSelect(cmp, llvm.ConstInt(xt, 0, false), shl, "")
	case ssa.OpRsh8x8, ssa.OpRsh8x16, ssa.OpRsh8x32, ssa.OpRsh8x64,
		ssa.OpRsh16x8, ssa.OpRsh16x16, ssa.OpRsh16x32, ssa.OpRsh16x64,
		ssa.OpRsh32x8, ssa.OpRsh32x16, ssa.OpRsh32x32, ssa.OpRsh32x64,
		ssa.OpRsh64x8, ssa.OpRsh64x16, ssa.OpRsh64x32, ssa.OpRsh64x64:
		step := b.CreateSelect(cmp, maxbits, y, "")
		// step is non-negative and smaller than maxbits. So we can safely extend it to
		// any supported integer type, i.e., i16, i32, and i64, either signed or unsigned.
		// AShr can produce a poison LLVM value. Hope LLVM can guarantee that if the shift
		// is smaller than maxbtis, the AShr will not generate a poison value.
		return b.CreateAShr(x, b.integerConvert(step, xt, signed), "")
	case ssa.OpRsh8Ux8, ssa.OpRsh8Ux16, ssa.OpRsh8Ux32, ssa.OpRsh8Ux64,
		ssa.OpRsh16Ux8, ssa.OpRsh16Ux16, ssa.OpRsh16Ux32, ssa.OpRsh16Ux64,
		ssa.OpRsh32Ux8, ssa.OpRsh32Ux16, ssa.OpRsh32Ux32, ssa.OpRsh32Ux64,
		ssa.OpRsh64Ux8, ssa.OpRsh64Ux16, ssa.OpRsh64Ux32, ssa.OpRsh64Ux64:
		shr := b.CreateLShr(x, b.integerConvert(y, xt, false), "")
		return b.CreateSelect(cmp, llvm.ConstInt(xt, 0, false), shr, "")
	}
	base.FatalfAt(expr.Pos, "unsupported: %v", expr)
	return llvm.Value{}
}

func (b *builder) getIntegerOperandsForBinaryOp(arg1, arg2 *ssa.Value) (x, y llvm.Value) {
	x, y = b.getValue(arg1), b.getValue(arg2)
	xtype, ytype := x.Type(), y.Type()
	if xtype.TypeKind() != llvm.IntegerTypeKind || ytype.TypeKind() != llvm.IntegerTypeKind {
		// not two integer operands
		return x, y
	}

	xWidth := x.Type().IntTypeWidth()
	yWidth := y.Type().IntTypeWidth()
	if xWidth == yWidth {
		return x, y
	}

	if xWidth > yWidth {
		yIsSigned := arg2.Type.IsSigned()
		y = b.integerConvert(y, x.Type(), yIsSigned)
	} else {
		xIsSigned := arg1.Type.IsSigned()
		x = b.integerConvert(x, y.Type(), xIsSigned)
	}
	return x, y
}

func (b *builder) createBinOp(expr *ssa.Value) llvm.Value {
	x, y := b.getIntegerOperandsForBinaryOp(expr.Args[0], expr.Args[1])
	switch expr.Op {
	case ssa.OpAdd8, ssa.OpAdd16, ssa.OpAdd32, ssa.OpAdd64:
		return b.CreateAdd(x, y, "")
	case ssa.OpAddPtr:
		x = b.toPtr(x, b.i8ptrType)
		x = b.createGEPFromFrontend(b.int8Type, x, []llvm.Value{y}, "")
		return b.toPtr(x, b.getLLVMType(expr.Type))
	case ssa.OpAdd32F, ssa.OpAdd64F:
		return b.CreateFAdd(x, y, "")
	case ssa.OpSub8, ssa.OpSub16, ssa.OpSub32, ssa.OpSub64:
		return b.CreateSub(x, y, "")
	case ssa.OpSubPtr:
		x = b.toPtr(x, b.i8ptrType)
		y = b.CreateNeg(y, "")
		x = b.createGEPFromFrontend(b.int8Type, x, []llvm.Value{y}, "")
		return b.toPtr(x, b.getLLVMType(expr.Type))
	case ssa.OpSub32F, ssa.OpSub64F:
		return b.CreateFSub(x, y, "")
	case ssa.OpMul8, ssa.OpMul16, ssa.OpMul32, ssa.OpMul64:
		return b.CreateMul(x, y, "")
	case ssa.OpMul32F, ssa.OpMul64F:
		return b.CreateFMul(x, y, "")
	case ssa.OpDiv32F, ssa.OpDiv64F:
		return b.CreateFDiv(x, y, "")
	case ssa.OpHmul32, ssa.OpHmul32u:
		shouldNotReachHere()
	case ssa.OpHmul64:
		if b.Config.CPU != llvm.CPU_X86_64 {
			return b.CreateMul(x, y, "")
		}
		// return type [llvm_i64_ty, llvm_i64_ty]
		return b.createIntrinsicCallWithArgs("llvm.go.hmul64", b.getArgs(expr), b.getLLVMType(expr.Type))
	case ssa.OpHmul64u:
		if b.Config.CPU != llvm.CPU_X86_64 {
			return b.CreateMul(x, y, "")
		}
		// return type [llvm_i64_ty, llvm_i64_ty]
		return b.createIntrinsicCallWithArgs("llvm.go.hmul64u", b.getArgs(expr), b.getLLVMType(expr.Type))
	case ssa.OpMul32uhilo:
		// 32bit
		shouldNotReachHere()
	case ssa.OpMul64uhilo:
		// we only support amd64 now.
		if b.Config.CPU != llvm.CPU_X86_64 {
			panic("TODO")
		}
		// return type [llvm_i64_ty, llvm_i64_ty]
		return b.createIntrinsicCallWithArgs("llvm.go.mul64uhilo", b.getArgs(expr), b.getLLVMType(expr.Type))
	case ssa.OpMul32uover:
		// 32bit
		shouldNotReachHere()
	case ssa.OpMul64uover:
		// we only support amd64 now.
		if b.Config.CPU != llvm.CPU_X86_64 {
			panic("TODO")
		}
		return b.createIntrinsicCallWithArgs("llvm.umul.with.overflow.i64", b.getArgs(expr),
			b.ctx.StructType([]llvm.Type{b.int64Type, b.int1Type}, false))
	case ssa.OpAvg32u:
		// 32bit
		shouldNotReachHere()
	case ssa.OpAvg64u:
		// we only support amd64 now.
		if b.Config.CPU != llvm.CPU_X86_64 {
			panic("TODO")
		}
		// return 64-bit average integer of two operands.
		return b.createIntrinsicCallWithArgs("llvm.go.avg64u", b.getArgs(expr), b.getLLVMType(expr.Type))
	case ssa.OpDiv8, ssa.OpDiv16, ssa.OpDiv32, ssa.OpDiv64:
		// TODO:
		// (lfk) add overflow for sdiv.
		bitWidth := x.Type().IntTypeWidth()
		minValue := uint64(1) << (bitWidth - 1)
		maxValue := (uint64(1) << (bitWidth - 1)) - 1
		minConst := llvm.ConstInt(x.Type(), minValue, false)
		maxConst := llvm.ConstInt(x.Type(), maxValue, false)
		isMinusOne := b.CreateICmp(llvm.IntEQ, y, llvm.ConstAllOnes(y.Type()), "")
		isIntMin := b.CreateICmp(llvm.IntEQ, x, minConst, "")
		willOverflow := b.CreateAnd(isMinusOne, isIntMin, "")
		safeDividend := b.CreateSelect(willOverflow, maxConst, x, "")
		divResult := b.CreateSDiv(safeDividend, y, "")
		finalResult := b.CreateSelect(willOverflow, minConst, divResult, "")
		return finalResult
	case ssa.OpDiv8u, ssa.OpDiv16u, ssa.OpDiv32u, ssa.OpDiv64u:
		// TODO:
		return b.CreateUDiv(x, y, "")
	case ssa.OpMod8, ssa.OpMod16, ssa.OpMod32, ssa.OpMod64:
		// TODO:
		// (lfk) add overflow for srem.
		isMinusOne := b.CreateICmp(llvm.IntEQ, y, llvm.ConstAllOnes(y.Type()), "")
		safeDivisor := b.CreateSelect(isMinusOne, llvm.ConstInt(x.Type(), 1, false), y, "")
		return b.CreateSelect(isMinusOne, llvm.ConstInt(x.Type(), 0, false), b.CreateSRem(x, safeDivisor, ""), "")
	case ssa.OpMod8u, ssa.OpMod16u, ssa.OpMod32u, ssa.OpMod64u:
		// TODO:
		return b.CreateURem(x, y, "")
	case ssa.OpAnd8, ssa.OpAnd16, ssa.OpAnd32, ssa.OpAnd64:
		return b.CreateAnd(x, y, "")
	case ssa.OpOr8, ssa.OpOr16, ssa.OpOr32, ssa.OpOr64:
		return b.CreateOr(x, y, "")
	case ssa.OpXor8, ssa.OpXor16, ssa.OpXor32, ssa.OpXor64:
		return b.CreateXor(x, y, "")
	case ssa.OpEq8, ssa.OpEq16, ssa.OpEq32, ssa.OpEq64:
		return b.CreateICmp(llvm.IntEQ, x, y, "")
	case ssa.OpEqPtr:
		// TODO: x and y should be in the opaque pointers
		// Here we use toPtr in case any of them is integer
		x = b.toPtr(x, b.ptrType)
		y = b.toPtr(y, b.ptrType)
		return b.CreateICmp(llvm.IntEQ, x, y, "")
	case ssa.OpEqInter:
		return b.CreateICmp(llvm.IntEQ, b.CreateExtractValue(x, 0, ""), b.CreateExtractValue(y, 0, ""), "")
	case ssa.OpEqSlice:
		return b.CreateICmp(llvm.IntEQ, b.CreateExtractValue(x, 0, ""), b.CreateExtractValue(y, 0, ""), "")
	case ssa.OpEq32F, ssa.OpEq64F:
		// any comparison involving a NaN is always TRUE on amd64
		// so we can't use a != b, but use !(a == b)
		return b.CreateNot(b.CreateFCmp(llvm.FloatUNE, x, y, ""), "")
	case ssa.OpNeq8, ssa.OpNeq16, ssa.OpNeq32, ssa.OpNeq64:
		return b.CreateICmp(llvm.IntNE, x, y, "")
	case ssa.OpNeqPtr:
		x = b.toPtr(x, b.ptrType)
		y = b.toPtr(y, b.ptrType)
		return b.CreateICmp(llvm.IntNE, x, y, "")
	case ssa.OpNeqInter:
		return b.CreateICmp(llvm.IntNE, b.CreateExtractValue(x, 0, ""), b.CreateExtractValue(y, 0, ""), "")
	case ssa.OpNeqSlice:
		return b.CreateICmp(llvm.IntNE, b.CreateExtractValue(x, 0, ""), b.CreateExtractValue(y, 0, ""), "")
	case ssa.OpNeq32F, ssa.OpNeq64F:
		return b.CreateFCmp(llvm.FloatUNE, x, y, "")
	case ssa.OpLess8, ssa.OpLess16, ssa.OpLess32, ssa.OpLess64:
		return b.CreateICmp(llvm.IntSLT, x, y, "")
	case ssa.OpLess8U, ssa.OpLess16U, ssa.OpLess32U, ssa.OpLess64U:
		return b.CreateICmp(llvm.IntULT, x, y, "")
	case ssa.OpLess32F, ssa.OpLess64F:
		return b.CreateNot(b.CreateFCmp(llvm.FloatUGE, x, y, ""), "")
	case ssa.OpLeq8, ssa.OpLeq16, ssa.OpLeq32, ssa.OpLeq64:
		return b.CreateICmp(llvm.IntSLE, x, y, "")
	case ssa.OpLeq8U, ssa.OpLeq16U, ssa.OpLeq32U, ssa.OpLeq64U:
		return b.CreateICmp(llvm.IntULE, x, y, "")
	case ssa.OpLeq32F, ssa.OpLeq64F:
		return b.CreateNot(b.CreateFCmp(llvm.FloatUGT, x, y, ""), "")
	case ssa.OpAndB:
		return b.CreateAnd(x, y, "")
	case ssa.OpOrB:
		return b.CreateOr(x, y, "")
	case ssa.OpEqB:
		return b.CreateICmp(llvm.IntEQ, x, y, "")
	case ssa.OpNeqB:
		return b.CreateICmp(llvm.IntNE, x, y, "")
	}
	base.FatalfAt(expr.Pos, "unsupported binop: %v", expr)
	return llvm.Value{}
}

type stringStruct struct {
	ptr *uint8
	len int
}

func (s *stringStruct) at(i int) uint8 {
	if i < 0 || i >= s.len {
		panic("out of bounds")
	}
	return *(*uint8)(unsafe.Pointer((uintptr)(unsafe.Pointer(s.ptr)) + uintptr(i)))
}

type ifaceStruct struct {
	tab  unsafe.Pointer
	data unsafe.Pointer
}

// createConst creates a LLVM constant value from a Go constant.
func (c *compilerContext) createConst(expr *ssa.Value) llvm.Value {
	switch expr.Op {
	case ssa.OpConstBool:
		return llvm.ConstInt(c.int1Type, uint64(expr.AuxInt), false)
	case ssa.OpConst8:
		return llvm.ConstInt(c.int8Type, expr.AuxUnsigned(), false)
	case ssa.OpConst16:
		return llvm.ConstInt(c.int16Type, expr.AuxUnsigned(), false)
	case ssa.OpConst32:
		return llvm.ConstInt(c.int32Type, expr.AuxUnsigned(), false)
	case ssa.OpConst64:
		return llvm.ConstInt(c.int64Type, expr.AuxUnsigned(), false)
	case ssa.OpConst32F:
		return llvm.ConstFloat(c.f32Type, expr.AuxFloat())
	case ssa.OpConst64F:
		return llvm.ConstFloat(c.f64Type, expr.AuxFloat())
	case ssa.OpConstString:
		ss := (*stringStruct)((*ifaceStruct)(unsafe.Pointer(&expr.Aux)).data)
		buf := make([]llvm.Value, ss.len)
		for i := 0; i < ss.len; i++ {
			buf[i] = llvm.ConstInt(c.int8Type, uint64(ss.at(i)), false)
		}
		return llvm.ConstNamedStruct(c.getLLVMType(expr.Type),
			[]llvm.Value{llvm.ConstArray(c.int8Type, buf), llvm.ConstInt(c.intType, uint64(ss.len), false)})
	case ssa.OpConstNil:
		// ConstNil <uintptr> should be considered as int type
		// example:
		// func: github.com/miekg/dns.tsigBuffer
		// v676 = ConstNil <uintptr>
		// v536 = Copy <uintptr> v676
		if expr.Type.Kind() == types.TUINTPTR {
			return llvm.ConstInt(c.uintptrType, 0, false)
		}
		return llvm.ConstNull(c.i8ptrType)
	case ssa.OpConstInterface:
		t := c.getLLVMType(expr.Type)
		ts := t.StructElementTypes()
		return llvm.ConstNamedStruct(c.getLLVMType(expr.Type),
			[]llvm.Value{
				llvm.ConstNull(ts[0]),
				llvm.ConstNull(ts[1]),
			})
	case ssa.OpConstSlice:
		t := c.getLLVMType(expr.Type)
		ts := t.StructElementTypes()
		return llvm.ConstNamedStruct(c.getLLVMType(expr.Type),
			[]llvm.Value{
				llvm.ConstNull(ts[0]),
				llvm.ConstInt(ts[1], 0, false),
				llvm.ConstInt(ts[2], 0, false),
			})
	}
	base.FatalfAt(expr.Pos, "unknown constant: %v", expr)
	return llvm.Value{}
}

// createCopy
// doing bitcast conversion
func (b *builder) createCopy(expr *ssa.Value) llvm.Value {
	if expr.Op != ssa.OpCopy && expr.Op != ssa.OpRound32F && expr.Op != ssa.OpRound64F {
		base.FatalfAt(expr.Pos, "must be OpCopy, got %v", expr)
	}
	if expr.Args[0].Type == types.TypeMem {
		return llvm.Value{}
	}
	x := b.getValue(expr.Args[0])
	llvmTypeTo := b.getLLVMType(expr.Type)
	return b.patchValueType(x, llvmTypeTo)
}

// createUnOp creates LLVM IR for a given Go unary operation.
// Most unary operators are pretty simple, such as the not and minus operator
// which can all be directly lowered to IR. However, there is also the channel
// receive operator which is handled in the runtime directly.
func (b *builder) createUnOp(unop *ssa.Value) llvm.Value {
	x := b.getValue(unop.Args[0])
	switch unop.Op {
	case ssa.OpNot: // !x
		return b.CreateNot(x, "")
	case ssa.OpNeg8, ssa.OpNeg16, ssa.OpNeg32, ssa.OpNeg64: // -x
		return b.CreateNeg(x, "")
	case ssa.OpNeg32F, ssa.OpNeg64F:
		return b.CreateFNeg(x, "")
	case ssa.OpCom8, ssa.OpCom16, ssa.OpCom32, ssa.OpCom64:
		return b.CreateXor(x, llvm.ConstInt(x.Type(), ^uint64(0), false), "")
	}
	base.FatalfAt(unop.Pos, "unknown unop: %v", unop)
	return llvm.Value{}
}

// ------- errors.go
func (b *builder) Fatalf(msg string, args ...interface{}) {
	base.FatalfAt(b.curPos, msg, args...)
}

func (c *builder) addStandardDefinedAttributes(llvmFn llvm.Value) {
	// we use variable c.inlinable to denote if a function is safe to inline or
	// not. Value of c.inlinable is determined during traversal of the IR. Note
	// that c.inlinable is indepedent of user's Inline=x flag; the inline attributes
	// (alwaysinline or inlinehint) will only be added if inlinable is true _and_
	// the user enabled Inline (i.e., Inline=1 or 2).
	// (fengkai) use Inline=1 by default.
	if c.inlinable && base.Flag.Cfg.BeastOpt.Inline > 0 {
		c.summary.inlinedFunc++
		if base.Flag.Cfg.BeastOpt.Inline > 1 {
			llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("alwaysinline"), 0))
		} else {
			llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("inlinehint"), 0))
		}
	} else {
		llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("noinline"), 0))
	}

	llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("no-jump-tables", "true"))
	llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("null_pointer_is_valid"), 0))
}

func (c *builder) addTargetFeatureAttributes(llvmFn llvm.Value) {
	if c.Features != "" {
		llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("target-features", c.Features))
	}
}

func (b *builder) createGEPFromFrontend(t llvm.Type, p llvm.Value, indices []llvm.Value, name string) (v llvm.Value) {
	gep := b.CreateGEP(t, p, indices, name)
	if !gep.IsAInstruction().IsNil() {
		// p may be a global and gep is a constant as well
		gep.SetMetadata(b.ctx.MDKindID("go.gep"), b.ctx.MDNode(nil))
	}
	return gep
}

// The arg is on stack. We first calculate its location based on offset and frame address
// and then do a loading.
func (b *builder) getArg(expr *ssa.Value) llvm.Value {
	if expr.Op != ssa.OpArg {
		base.FatalfAt(expr.Pos, "must be OpArg, got %v", expr)
	}
	n := expr.Aux.(*ir.Name)
	o := expr.AuxInt
	addr := b.getOnStackArg(n)
	if o > 0 {
		addr = b.createGEPFromFrontend(b.int8Type, addr, []llvm.Value{llvm.ConstInt(b.int64Type, uint64(o), false)}, n.Sym().Name)
	}
	llvmTy := b.getLLVMType(expr.Type)
	t := llvm.PointerType(llvmTy, 0)
	casted := b.toPtr(addr, t)
	l := b.CreateLoad(llvmTy, casted, n.Sym().Name)
	return l
}

func (b *builder) getArgReg(expr *ssa.Value) llvm.Value {
	if expr.Op != ssa.OpArgIntReg && expr.Op != ssa.OpArgFloatReg {
		base.FatalfAt(expr.Pos, "must be OpArgIntReg or OpArgFloatReg, got %v", expr)
	}
	regIdx := expr.AuxInt
	argIdx := 0

	for _, pa := range b.f.OwnAux.ABIInfo().InParams() {
		if len(pa.Registers) == 0 {
			continue
		}
		for _, r := range pa.Registers {
			op, auxInt := ssa.ArgOpAndRegisterFor(r, b.f.ABISelf)
			if op == expr.Op {
				if auxInt == regIdx {
					arg := b.llvmFn.Param(argIdx)
					if argIdx >= b.llvmFn.ParamsCount() {
						base.FatalfAt(expr.Pos, "out of bounds: %d >= %d", argIdx, b.llvmFn.ParamsCount())
					}
					if arg.IsNil() {
						base.FatalfAt(expr.Pos, "param at %d is nil for %s in func %s", argIdx, expr.LongString(), b.llvmFn.Type().ElementType().String())
					}
					return arg
				}
			}
			argIdx++
		}
	}
	base.FatalfAt(expr.Pos, "cannot find arg for index %v and op %v", regIdx, expr.Op)
	return llvm.Value{}
}

// Computing gcdata is tedious. We can do it in LLVM backend but the easiest way
// is to compute the gcdata in Tango and pass it to the LLVM backend.
func (b *builder) addStackObjectGCData(n *ir.Name, v llvm.Value) {
	// only PAUTO will be mapped to AllocaInst
	// PPARAM and PPARAMOUT are in preallocated.
	if n.Class == ir.PAUTO && n.Addrtaken() && n.Type().HasPointers() && n.Esc() != ir.EscHeap {
		// stack objects
		t := n.Type()
		sz := t.Size()
		lsym, ptrdata := reflectdata.GCSym(t, false)
		mdid := b.ctx.MDKindID("go.stackobject.gcdata")
		mdsize := llvm.ConstInt(b.int32Type, uint64(sz), false).ConstantAsMetadata()
		mdptrdata := llvm.ConstInt(b.int32Type, uint64(ptrdata), false).ConstantAsMetadata()
		mdsym := b.ctx.MDString(lsym.Name)
		mdarray := []llvm.Metadata{mdsize, mdptrdata, mdsym}
		v.SetMetadata(mdid, b.ctx.MDNode(mdarray))
	}
}

func (b *builder) getLLVMEntryBlock() llvm.BasicBlock {
	entryLLVMBlock := b.blocks[b.f.Entry]
	if entryLLVMBlock != b.llvmFn.FirstBasicBlock() {
		panic("must be the first basic block")
	}
	return entryLLVMBlock
}

func (b *builder) createGoPreAllocated(t llvm.Type, args ...llvm.Value) llvm.Value {
	// Caller-owned slots are not ordinary local allocas: output stores must
	// remain observable and input slots contain values supplied by the caller.
	// The null backing pointer selects the already-legalized representation;
	// instruction selection resolves the explicit frame offset.
	ptr := llvm.PointerType(t, 0)
	addr := llvm.ConstNull(ptr)
	args = append([]llvm.Value{addr}, args...)
	return b.CreateIntrinsic(b.goPreAllocatedID, []llvm.Type{ptr, ptr, t}, args, "")
}

func (b *builder) getOnStackArg(n *ir.Name) llvm.Value {
	if a, ok := b.locals[n]; ok {
		return a
	}
	offset := b.FrameOffset(n)
	size := n.Type().Size()
	b.inlinable = false
	// the current insert point must be end of the BB
	bb := b.GetInsertBlock()
	// generate all preallocated at the beginning of entry block
	entry := b.getLLVMEntryBlock()
	fi := entry.FirstInstruction()
	if !fi.IsNil() {
		b.SetInsertPointBefore(fi)
	} else {
		b.SetInsertPointAtEnd(entry)
	}
	defer b.SetInsertPointAtEnd(bb)
	// TODO: create a single copy of preallocated at the entry block
	et := b.getLLVMType(n.Type())
	addr := b.createGoPreAllocated(et, llvm.ConstInt(b.int64Type, uint64(offset), false),
		llvm.ConstInt(b.int64Type, uint64(size), false),
		b.getZeroValueConst(et),
		llvm.ConstInt(b.int1Type, uint64(0), false))
	// cache results
	b.locals[n] = addr
	return addr
}

// We cannot use Alloca for locals in arg area, in the caller frame
// because these values are mostly used without initialization (except PPARAMOUT?)
// We will create a large unused Alloca for the whole arg area (see (*builder).argAreaType).
// So we cannot allocate an individual Alloca for PPARAMOUT
func (b *builder) getLocal(expr *ssa.Value) llvm.Value {
	n := expr.Aux.(*ir.Name)
	switch n.Class {
	case ir.PAUTO, ir.PPARAMOUT:
		if a, ok := b.locals[n]; ok {
			return a
		}
		base.FatalfAt(expr.Pos, "cannot find local variable for register allocated PARAMOUT %v", n)
		return llvm.Value{}
	case ir.PPARAM:
		addr := b.getOnStackArg(n)
		t := b.getLLVMType(expr.Type)
		if t.TypeKind() != llvm.PointerTypeKind {
			base.FatalfAt(expr.Pos, "The type of LocalAddr must be a pointer, but we got %v", t.TypeKind())
		}
		return b.toPtr(addr, t)
	default:
		base.FatalfAt(expr.Pos, "Cannot get local for %v", expr)
	}
	return llvm.Value{}
}

// Return a LLVMGlobal
// As are extern with external linkage.
// NOTE: we have no need to create debug info for global because all globals are actually
// handled and defined by the gc compiler. The LLVM global here is simply a symbol placeholder.
func (b *builder) getGlobal(expr *ssa.Value) llvm.Value {
	c := b.compilerContext
	if expr.Op != ssa.OpAddr && expr.Op != ssa.OpHasCPUFeature {
		base.FatalfAt(expr.Pos, "unsupported Op: %v", expr)
	}
	s := expr.Aux.(*obj.LSym)
	linkname := s.Name
	global := c.mod.NamedGlobal(linkname)
	if !global.IsNil() {
		return global
	}

	llvmType := c.getLLVMType(expr.Type)
	if expr.Op == ssa.OpAddr {
		// TODO: we observed that the type of the address of a global variable
		// may be uintptr
		switch expr.Type.Kind() {
		case types.TUINTPTR:
		case types.TUNSAFEPTR:
			llvmType = c.int8Type // any byte stream
		case types.TPTR:
			llvmType = c.getLLVMType(expr.Type.Elem())
		default:
			base.FatalfAt(expr.Pos, "The type of the address of a global variable is not pointer shaped: %s.", expr.LongString())
		}
	}

	if llvmFn := c.mod.NamedFunction(linkname); !llvmFn.IsNil() {
		return b.toPtr(llvmFn, llvm.PointerType(llvmType, 0))
	}

	global = llvm.AddGlobal(c.mod, llvmType, linkname)
	global.SetLinkage(llvm.ExternalLinkage)

	if s.Type == objabi.SRODATA {
		if len(s.R) == 0 {
			mdid := b.ctx.MDKindID("go.rodata")
			mddata := b.ctx.MDString(string(s.P))
			global.GlobalSetMetadata(mdid, b.ctx.MDNode([]llvm.Metadata{mddata}))
		}
	}
	return global
}

func (c *compilerContext) getGoFunctionLLVMName(sym *obj.LSym) string {
	return c.getFunctionLLVMName2(sym.Name, sym.ABI())
}

func (c *compilerContext) getFunctionLLVMName2(name string, abi obj.ABI) string {
	// this is used for ELF and MachO only since
	// there is no ABI info in symbols in ELF and MachO.
	return llvm.EncodeFuncNameAndABI(name, abi)
}

func (c *compilerContext) getFunctionTypesByAuxCall(aux *ssa.AuxCall) llvm.Type {
	paramTypes := c.getFunctionParameterTypesByAuxCall(aux)
	retType := c.getFunctionRetTypeByAuxCall(aux)
	return llvm.FunctionType(retType, paramTypes, false)
}

func (c *compilerContext) getFunctionParameterTypesByAuxCall(aux *ssa.AuxCall) []llvm.Type {
	abiInfo := aux.ABIInfo()
	paramTypes := make([]llvm.Type, 0, abiInfo.InRegistersUsed())

	for _, p := range abiInfo.InParams() {
		typs, _ := p.RegisterTypesAndOffsets()
		for _, t := range typs {
			paramTypes = append(paramTypes, c.getLLVMType(t))
		}
	}
	return paramTypes
}

func (c *compilerContext) getFunctionRetTypeByAuxCall(aux *ssa.AuxCall) (retType llvm.Type) {
	abiInfo := aux.ABIInfo()
	tys := abi.RegisterTypes(abiInfo.OutParams())
	switch len(tys) {
	case 0:
		if aux.Fn == ir.Syms.DeferprocStack || aux.Fn == ir.Syms.Deferproc {
			// TODO: remove Defer related stuffs
			retType = c.ctx.Int1Type()
			return
		}
		// void type and on-stack return value
		retType = c.ctx.VoidType()
	case 1:
		// single simple type
		retType = c.getLLVMType(tys[0])
	default:
		// structure or tuple
		retType = c.getLLVMType(types.NewResults(append(tys, types.TypeMem)))
	}
	return
}

func (b *builder) getFunctionByAuxCallABI(aux *ssa.AuxCall) (llvm.Value, llvm.Type) {
	paramTypes := b.getFunctionParameterTypesByAuxCall(aux)
	retType := b.getFunctionRetTypeByAuxCall(aux)
	if aux.Fn == nil {
		panic("must be StaticAuxCall or OwnAuxCall")
	}
	return b.getOrCreateGoFunction(aux.Fn, paramTypes, retType)
}

// getFunction returns the LLVM function for the given *ssa.Function, creating
// it if needed. It can later be filled with compilerContext.createFunction().
func (b *builder) getFunction(fn *ssa.Func) llvm.Value {
	llvmFn, _ := b.getFunctionByAuxCallABI(fn.OwnAux)
	return llvmFn
}
