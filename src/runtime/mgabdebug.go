// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	_ "unsafe"
)

//go:linkname dbgIsInGabSpan
func dbgIsInGabSpan(p uintptr) bool {
	s := spanOf(p)
	if s != nil {
		return s.isGab()
	}
	return false
}

//go:linkname dbgGabPin
func dbgGabPin(p uintptr) {
	s := spanOf(p)
	if s == nil {
		panic("Not in a span!")
	}
	if !s.isGab() {
		panic("Not a GAB span")
	}
	s.gabPinObj(p)
}

//go:linkname dbgIsGabPinned
func dbgIsGabPinned(p uintptr) bool {
	s := spanOf(p)
	if s == nil {
		panic("Not in a span!")
	}
	if !s.isGab() {
		return false
	}
	return s.isPinnedGab(p)
}

//go:linkname dbgDumpGABObject
func dbgDumpGABObject(label string, p uintptr) {
	s := spanOf(p)
	if !s.isGab() {
		println("Not a GAB span")
		return
	}
	obj := s.base() + s.objIndex(p)*s.elemsize
	offset := p - obj
	gcDumpObject(label, obj, offset)
}
