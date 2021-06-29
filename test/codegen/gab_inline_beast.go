// asmcheck -opt=beast

// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package codegen tests whether the code is generated as expected
package codegen

import "unsafe"

// This file contains code generation tests related to gab inline.
// On newobject, runtime.gabEnabled will only be called
// when gab inline happends. Thus, it uses `.*runtime\.gabEnabled`
// to identify whether a gab inline happens.

var refs []unsafe.Pointer

// GabInline tests whether the gab inline feature can correctly handle the newobject.
// It also tests whether the hardcoded value is as expected.
// 464 and 472 should match the offset of gp.ab.top and gp.ab.end in src/runtime/mgab.go and src/cmd/compiler/internal/ssa.go
// 128 should match the src/runtime/mgab.go:gabMaxAllocSize and src/cmd/compiler/internal/ssa.go:gabMaxAllocSize
func GabInline() {
	refs = make([]unsafe.Pointer, 4)
	// amd64:`.*runtime\.gabEnabled`
	// amd64:`MOVQ\s*464\(R14\), [A-Z]{2}`
	// amd64:`MOVQ\s*472\(R14\), [A-Z]{2}`
	refs[0] = unsafe.Pointer(new([2]int))
	// amd64:`.*runtime\.gabEnabled`
	refs[1] = unsafe.Pointer(&[2]int{})
	// amd64:`.*runtime\.gabEnabled`
	refs[2] = unsafe.Pointer(&(new([128]byte)[0]))
	// amd64:-`.*runtime\.gabEnabled`
	refs[3] = unsafe.Pointer(&(new([129]byte)[0]))
	// amd64:-`.*runtime\.gabEnabled`
	c := new([2]int) // not inline: c is not escaped
	c[0] = 0
}
