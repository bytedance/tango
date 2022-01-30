// asmcheck

// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen

import "unsafe"

// This file contains code generation tests related to gab inline.
// On newobject, runtime.gabEnabled will only be called
// when gab inline happends. Thus, it uses `.*runtime\.gabInliningEnabled`
// to identify whether a gab inline happens.

var refs []unsafe.Pointer

// GabNotInline tests whether the gab inline turns off for the newobject when beastmode flag is not set.
func GabNotInline() {
	refs = make([]unsafe.Pointer, 2)
	// amd64:-`.*runtime\.gabInliningEnabled`
	refs[0] = unsafe.Pointer(new([2]int))
	// amd64:-`.*runtime\.gabInliningEnabled`
	refs[1] = unsafe.Pointer(&[2]int{})
}
