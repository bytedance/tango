// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package maps

import (
	"internal/abi"
	"unsafe"
)

//go:linkname tcTypeToTypePointer runtime.tcTypeToTypePointer
func tcTypeToTypePointer(c *tcConverter, from, to unsafe.Pointer, ft, tt *abi.Type) error

//go:linkname tcDereferenceWithNewObject runtime.tcDereferenceWithNewObject
func tcDereferenceWithNewObject(p unsafe.Pointer, typ *abi.Type) unsafe.Pointer

//go:linkname tcBucketToBucketDiff runtime.tcBucketToBucketDiff
func tcBucketToBucketDiff(c *tcConverter, ft, tt *abi.MapType, fh, th *Map) error {
	if fh.dirLen == 0 {
		return tcGroupToGroupDiff(c, ft, tt, fh.dirPtr, th)
	}

	for ti := 0; ti < fh.dirLen; ti++ {
		ftab := fh.directoryAt(uintptr(ti))
		s := ftab.groups.lengthMask
		for bi := uint64(0); bi <= s; bi++ {
			fb := ftab.groups.group(ft, bi)
			if fb.ctrls().matchFull() == 0 {
				continue
			}
			if err := tcGroupToGroupDiff(c, ft, tt, fb.data, th); err != nil {
				return err
			}
		}
	}

	return nil
}

func tcGroupToGroupDiff(c *tcConverter, ft *abi.MapType, tt *abi.MapType, fd unsafe.Pointer, th *Map) error {
	fb := groupReference{data: fd}
	for fi := uintptr(0); fi < abi.MapGroupSlots; fi++ {
		top := fb.ctrls().get(fi)
		if top&ctrlEmpty != 0 {
			continue
		}
		fk := fb.key(ft, fi)
		if ft.IndirectKey() {
			fk = *(*unsafe.Pointer)(fk)
		}
		tk := newobject(tt.Key)
		err := tcTypeToTypePointer(c, fk, tk, ft.Key, tt.Key)
		if err != nil {
			return err
		}
		fe := fb.elem(ft, fi)
		if ft.IndirectElem() {
			fe = *(*unsafe.Pointer)(fe)
		}
		te := th.PutSlot(tt, tk)
		err = tcTypeToTypePointer(c, fe, te, ft.Elem, tt.Elem)
		if err != nil {
			return err
		}
	}
	return nil
}

//go:linkname tcBucketToBucketSame runtime.tcBucketToBucketSame
func tcBucketToBucketSame(c *tcConverter, ft, tt *abi.MapType, fh, th *Map) error {
	if fh.dirLen == 0 {
		err := tcGroupToGroupSame(c, ft, tt, fh.dirPtr, th.dirPtr)
		if err != nil {
			return err
		}
		th.used = fh.used
		return nil
	}

	for ti := 0; ti < fh.dirLen; ti++ {
		ftab := fh.directoryAt(uintptr(ti))
		ttab := th.directoryAt(uintptr(ti))
		s := ftab.groups.lengthMask
		for bi := uint64(0); bi <= s; bi++ {
			fb := ftab.groups.group(ft, bi)
			if fb.ctrls().matchFull() == 0 {
				continue
			}
			tb := ttab.groups.group(tt, bi)
			if err := tcGroupToGroupSame(c, ft, tt, fb.data, tb.data); err != nil {
				return err
			}
		}
		ttab.used = ftab.used
		ttab.growthLeft = ftab.growthLeft
	}
	th.used = fh.used
	return nil
}

func tcGroupToGroupSame(c *tcConverter, ft, tt *abi.MapType, fd, td unsafe.Pointer) error {
	fb := groupReference{data: fd}
	tb := groupReference{data: td}
	*tb.ctrls() = *fb.ctrls()
	for fi := uintptr(0); fi < abi.MapGroupSlots; fi++ {
		top := fb.ctrls().get(fi)
		if top&ctrlEmpty != 0 {
			continue
		}
		fk := fb.key(ft, fi)
		if ft.IndirectKey() {
			fk = *(*unsafe.Pointer)(fk)
		}

		tk := tb.key(tt, fi)
		if tt.IndirectKey() {
			tk = tcDereferenceWithNewObject(tk, tt.Key)
		}
		err := tcTypeToTypePointer(c, fk, tk, ft.Key, tt.Key)
		if err != nil {
			return err
		}
		fe := fb.elem(ft, fi)
		if ft.IndirectElem() {
			fe = *(*unsafe.Pointer)(fe)
		}
		te := tb.elem(tt, fi)
		if tt.IndirectElem() {
			te = tcDereferenceWithNewObject(te, tt.Elem)
		}
		err = tcTypeToTypePointer(c, fe, te, ft.Elem, tt.Elem)
		if err != nil {
			return err
		}
	}
	return nil
}
