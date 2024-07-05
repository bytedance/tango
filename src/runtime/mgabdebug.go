// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

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

//go:linkname dbgMakeGabStrHeaderForWBTest
func dbgMakeGabStrHeaderForWBTest() (bodyPtr, headerPtr uintptr) {
	body := new([1025]byte)
	s := unsafe.String((*byte)(unsafe.Pointer(body)), len(body))
	header := convTstring(s)
	return uintptr(unsafe.Pointer(body)), uintptr(header)
}

// dbgGabStrHeaderBodyAddrNeedsMallocHeader checks the address calculation used
// by wbBufFlush1 for a GAB-allocated string header. The string header Data
// field is not span.base()-relative when scan objects have malloc headers; it
// starts at the object base after mallocHeaderSize. The fixed calculation must
// find bodyPtr through that adjusted base, while the old span.base()-relative
// calculation must not find bodyPtr for this test case.
//
//go:linkname dbgGabStrHeaderBodyAddrNeedsMallocHeader
func dbgGabStrHeaderBodyAddrNeedsMallocHeader(bodyPtr, headerPtr uintptr) bool {
	bodySpan := spanOfHeap(bodyPtr)
	if bodySpan == nil || !bodySpan.spanclass.noscan() {
		return false
	}

	headerSpan := spanOfHeap(headerPtr)
	if headerSpan == nil || !headerSpan.isGab() || headerSpan.spanclass.noscan() {
		return false
	}
	headerIndex := headerSpan.objIndex(headerPtr)

	if *(*uintptr)(unsafe.Pointer(headerPtr)) != bodyPtr {
		return false
	}

	// The corrected formula first moves from the span base to this GAB
	// object's user data, then groups string headers by gabStrHeaderObjSize
	// within that malloc-header-adjusted object.
	baseAfterMallocHeader := headerSpan.base() + headerSpan.elemsize*headerIndex + mallocHeaderSize
	if headerPtr < baseAfterMallocHeader || (headerPtr-baseAfterMallocHeader)%gabStrHeaderObjSize != 0 {
		return false
	}
	bodyPtrAddr := baseAfterMallocHeader + (headerPtr-baseAfterMallocHeader)/gabStrHeaderObjSize*gabStrHeaderObjSize
	if bodyPtrAddr != headerPtr || *(*uintptr)(unsafe.Pointer(bodyPtrAddr)) != bodyPtr {
		return false
	}

	// The old formula grouped by gabStrHeaderObjSize from span.base(), which
	// can point at the malloc header or another word instead of the Data field.
	oldBodyPtrAddr := headerSpan.base() + (headerPtr-headerSpan.base())/gabStrHeaderObjSize*gabStrHeaderObjSize
	return oldBodyPtrAddr != bodyPtrAddr && *(*uintptr)(unsafe.Pointer(oldBodyPtrAddr)) != bodyPtr
}
