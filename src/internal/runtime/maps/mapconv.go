// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package maps

import (
	"internal/abi"
	"unsafe"
)

//go:linkname tcGetBucketOffset runtime.tcGetBucketOffset
func tcGetBucketOffset(m *Map, mt *abi.MapType, key string, hash uintptr) (uintptr, uintptr, uintptr, uint8, bool) {
	if m.dirLen == 0 {
		index, tophash, mustDelete := tcGetGroupOffset(m.dirPtr, mt, key, hash)
		if tophash&ctrlEmpty != 0 {
			panic("should not reach here")
		}
		return 0, 0, index, uint8(tophash), mustDelete
	}

	idx := m.directoryIndex(hash)
	tab := m.directoryAt(idx)
	seq := makeProbeSeq(h1(hash), tab.groups.lengthMask)
	for ; ; seq = seq.next() {
		g := tab.groups.group(mt, seq.offset)

		index, tophash, mustDelete := tcGetGroupOffset(g.data, mt, key, hash)
		if tophash&ctrlEmpty == 0 {
			return idx, uintptr(seq.offset), index, uint8(tophash), mustDelete
		}

		if g.ctrls().matchEmpty() != 0 {
			// Finding an empty slot means we've reached the end of
			// the probe sequence.
			panic("should not reach here")
		}
	}
}

func tcGetGroupOffset(d unsafe.Pointer, mt *abi.MapType, key string, hash uintptr) (uintptr, ctrl, bool) {
	g := groupReference{
		data: d,
	}

	match := g.ctrls().matchH2(h2(hash))

	for match != 0 {
		i := match.first()
		slotKey := g.key(mt, i)
		if mt.IndirectKey() {
			slotKey = *((*unsafe.Pointer)(slotKey))
		}
		if *(*string)(slotKey) == key {
			return i, g.ctrls().get(i), g.ctrls().matchEmpty() == 0
		}
		match = match.removeFirst()
	}
	return 0, ctrlEmpty, false
}

//go:linkname tcGetTablePointer runtime.tcGetTablePointer
func tcGetTablePointer(h *Map, idx uintptr) *table {
	return h.directoryAt(idx)
}

//go:linkname tcGetSeed runtime.tcGetSeed
func tcGetSeed(h *Map) uintptr {
	return h.seed
}

//go:linkname tcGetBucketPointer runtime.tcGetBucketPointer
func tcGetBucketPointer(tab *table, tt *abi.MapType, idx uintptr) unsafe.Pointer {
	return tab.groups.group(tt, uint64(idx)).data
}

//go:linkname tcHandleOverflow runtime.tcHandleOverflow
func tcHandleOverflow(th *Map, table, bucket, index uintptr, tt *abi.MapType) (
	uintptr, *table, unsafe.Pointer) {
	if th.dirLen == 0 {
		return index, nil, th.dirPtr
	}
	ttab := tcGetTablePointer(th, table)
	tb := tcGetBucketPointer(ttab, tt, bucket)
	return index, ttab, tb
}

//go:linkname tcSetHash runtime.tcSetHash
func tcSetHash(tb unsafe.Pointer, index uintptr, hash ctrl) {
	(&groupReference{data: tb}).ctrls().set(index, hash)
}

//go:linkname tcGetKeyOffset runtime.tcGetKeyOffset
func tcGetKeyOffset(index uintptr, mt *abi.MapType) uintptr {
	return groupSlotsOffset + index*mt.SlotSize
}

//go:linkname tcGetElemOffset runtime.tcGetElemOffset
func tcGetElemOffset(index uintptr, mt *abi.MapType) uintptr {
	return groupSlotsOffset + index*mt.SlotSize + mt.ElemOff
}

// tcHandleUsed can only be used to set the remaining capacity of the cloned map.
// Do not use it in other scenarios.
//
//go:linkname tcHandleUsed runtime.tcHandleUsed
func tcHandleUsed(th, fh *Map) {
	th.used = fh.used
	if th.dirLen == 0 {
		return
	}
	for ti := 0; ti < fh.dirLen; ti++ {
		ftab := fh.directoryAt(uintptr(ti))
		ttab := th.directoryAt(uintptr(ti))
		ttab.used = ftab.used
		ttab.growthLeft = ftab.growthLeft
	}
}

//go:linkname tcAllocMap runtime.tcAllocMap
func tcAllocMap(fh *Map, t *abi.MapType) *Map {
	if fh == nil {
		panic("fh is nil")
	}
	th := new(Map)
	th.seed = fh.seed

	if fh.used == 0 {
		return th // empty map.
	}

	th.globalDepth = fh.globalDepth
	th.globalShift = fh.globalShift
	th.dirLen = fh.dirLen
	if th.dirLen == 0 {
		th.growToSmall(t)
		return th
	}
	directory := make([]*table, fh.dirLen)

	for i := range directory {
		ftab := fh.directoryAt(uintptr(i))
		directory[i] = newTable(t, uint64(ftab.capacity), ftab.index, ftab.localDepth)
	}

	th.dirPtr = unsafe.Pointer(&directory[0])

	return th
}

//go:linkname tcHandleEmpty runtime.tcHandleEmpty
func tcHandleEmpty(tb unsafe.Pointer, index uintptr, th *Map, tt *table, mustDelete bool) {
	th.used--
	if tt != nil {
		tt.used--
	}
	g := groupReference{data: tb}
	if mustDelete {
		// We only ever mark the slot as deleted if the entry we want to delete
		// is in a pack of bucketCnt non-EMPTY buckets.
		g.ctrls().set(index, ctrlDeleted)
	} else {
		if tt != nil {
			tt.growthLeft++
		}
		g.ctrls().set(index, ctrlEmpty)
	}
}

type tcConverter struct {
	numberKind uint8
	deepCopy   bool
	context    tcEface
}

type tcEface struct {
	_type *abi.Type
	data  unsafe.Pointer
}

// key is stringToString
// ele is interfaceToInterface
// We depth-first convert each element in the from-map. If the result is the same as the original,
// then we can proceed to the next element. If the entire from-map is not actually converted,
// then we simply return from-map. If the result is different from the original element,
// we copy the from-map, assign the result to the corresponding location in the to-map, and return to-map.
func tcDeepCopyMapOnWrite(c *tcConverter, ft, tt *abi.MapType, fh *Map) (unsafe.Pointer, error) {
	if fh.dirPtr == nil {
		return unsafe.Pointer(fh), nil
	}
	var (
		mustDeepCopy bool
		th           *Map
		err          error
	)

	if fh.dirLen == 0 {
		g := groupReference{data: fh.dirPtr}
		for fi := uintptr(0); fi < abi.MapGroupSlots; fi++ {
			top := g.ctrls().get(fi)
			if top&ctrlEmpty != 0 {
				continue
			}
			mustDeepCopy, th, err = c.shallowCopyUtilDifferentType(ft, fh, fh.dirPtr, fi)
			if err != nil {
				return nil, err
			}
			if mustDeepCopy {
				break
			}
		}
		if mustDeepCopy {
			th.used = fh.used
			return unsafe.Pointer(th), nil
		}
		return unsafe.Pointer(fh), nil
	}

	for ti := 0; ti < fh.dirLen; ti++ {
		ftab := fh.directoryAt(uintptr(ti))
		s := ftab.groups.lengthMask
		for bi := uint64(0); bi <= s; bi++ {
			if !mustDeepCopy {
				fb := ftab.groups.group(ft, bi)
				if fb.ctrls().matchFull() == 0 {
					continue
				}
				for fi := uintptr(0); fi < abi.MapGroupSlots; fi++ {
					top := fb.ctrls().get(fi)
					if top&ctrlEmpty != 0 {
						continue
					}
					mustDeepCopy, th, err = c.shallowCopyUtilDifferentType(ft, fh, fb.data, fi)
					if err != nil {
						return nil, err
					}
					if mustDeepCopy {
						break
					}
				}
			} else {
				ttab := th.directoryAt(uintptr(ti))
				if err := c.convertBucketSameKey(ft, ftab.groups.group(ft, bi).data, ft,
					ttab.groups.group(tt, bi).data, nil, 0); err != nil {
					return nil, err
				}
				ttab.used = ftab.used
				ttab.growthLeft = ftab.growthLeft
			}
		}
	}

	if mustDeepCopy {
		th.used = fh.used
		return unsafe.Pointer(th), nil
	}
	return unsafe.Pointer(fh), nil
}

func (c *tcConverter) convertBucketSameKey(ft *abi.MapType, fgroup unsafe.Pointer, tt *abi.MapType,
	tgroup unsafe.Pointer, bmapUtil unsafe.Pointer, bmapIdxUtil uintptr) error {
	fb := groupReference{data: fgroup}
	if fb.ctrls().matchFull() == 0 {
		return nil
	}

	tb := groupReference{data: tgroup}
	bound := uintptr(abi.MapGroupSlots)
	if fb.data == bmapUtil {
		bound = bmapIdxUtil
	}
	*tb.ctrls() = *fb.ctrls()
	for fi := uintptr(0); fi < abi.MapGroupSlots; fi++ {
		top := fb.ctrls().get(fi)
		if top&ctrlEmpty != 0 {
			continue
		}

		if err := c.convertSameKeyAndEle(fb.data, tb.data, ft, tt, fi, fi, fi >= bound); err != nil {
			return err
		}
	}
	return nil
}

//go:linkname tcConvertAnyToString runtime.tcConvertAnyToString
func tcConvertAnyToString(c *tcConverter, from unsafe.Pointer, ft *abi.Type) (unsafe.Pointer, error)

func (c *tcConverter) convertBucketDiffKey(ft *abi.MapType, fh unsafe.Pointer, tt *abi.MapType,
	th *Map) error {
	fb := groupReference{data: fh}
	if fb.ctrls().matchFull() == 0 {
		return nil
	}

	for i := uintptr(0); i < abi.MapGroupSlots; i++ {
		top := fb.ctrls().get(i)
		if top&ctrlEmpty != 0 {
			continue
		}
		// top must >= minTopHash, a valid key/elem.
		k := fb.key(ft, i)
		if ft.IndirectKey() {
			k = *((*unsafe.Pointer)(k))
		}
		e := fb.elem(ft, i)
		if ft.IndirectElem() {
			e = *((*unsafe.Pointer)(e))
		}
		to, err := tcConvertAnyToString(c, k, ft.Key)
		if err != nil {
			return err
		}
		te := th.PutSlot(tt, to)
		ie := (*tcEface)(te)
		ie.data, ie._type, err = tcConvertAny(c, e, ft.Elem)
		if err != nil {
			return err
		}
	}
	return nil
}

//go:linkname tcConvertAny runtime.tcConvertAny
func tcConvertAny(c *tcConverter, p unsafe.Pointer, t *abi.Type) (unsafe.Pointer, *abi.Type, error)

func (c *tcConverter) shallowCopyUtilDifferentType(ft *abi.MapType, fh *Map, fb unsafe.Pointer, fi uintptr) (bool,
	*Map, error) {
	g := groupReference{data: fb}
	fe := g.elem(ft, fi)
	e := (*tcEface)(fe)
	var (
		err      error
		tmpEface tcEface
	)
	tmpEface.data, tmpEface._type, err = tcConvertAny(c, fe, ft.Elem)
	if err != nil {
		return false, nil, err
	}
	if tmpEface.data != e.data || tmpEface._type != e._type {
		th := tcAllocMap(fh, ft) // ft is same as tt.
		c.context = tmpEface
		return true, th, c.copyAndConvertMap(ft, fh, ft, th, fb, fi)
	}
	return false, nil, nil
}

func (c *tcConverter) copyAndConvertMap(ft *abi.MapType, fh *Map, tt *abi.MapType, th *Map,
	bmapUtil unsafe.Pointer, bmapIdxUtil uintptr) error {
	if fh.dirLen == 0 {
		return c.convertBucketSameKey(ft, fh.dirPtr, tt, th.dirPtr, bmapUtil, bmapIdxUtil)
	}

maploop:
	for ti := 0; ti < fh.dirLen; ti++ {
		ftab := fh.directoryAt(uintptr(ti))
		s := ftab.groups.lengthMask
		ttab := th.directoryAt(uintptr(ti))
		for bucket := uint64(0); bucket <= s; bucket++ {
			fb := ftab.groups.group(ft, bucket).data
			if fb == bmapUtil {
				if err := c.convertBucketSameKey(ft, fb, tt, ttab.groups.group(tt, bucket).data, bmapUtil, bmapIdxUtil); err != nil {
					return err
				}
				break maploop
			}
			if err := c.convertBucketSameKey(ft, fb, tt,
				ttab.groups.group(tt, bucket).data, nil, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

//go:linkname tcConvertMap runtime.tcConvertMap
func tcConvertMap(c *tcConverter, p unsafe.Pointer, ft, tt *abi.MapType) (unsafe.Pointer, error) {
	fh := *(**Map)(p)
	if fh == nil {
		return nil, nil
	}
	if !c.deepCopy && ft.Key == tt.Key && ft.Elem == tt.Elem {
		return tcDeepCopyMapOnWrite(c, ft, tt, fh)
	}
	th := tcAllocMap(fh, tt)

	return c.convertMapToMap(ft, fh, tt, th)
}

func (c *tcConverter) convertMapToMap(ft *abi.MapType, fh *Map, tt *abi.MapType, th *Map) (unsafe.Pointer,
	error) {
	if fh.dirPtr == nil {
		return unsafe.Pointer(th), nil
	}

	// stringToString.
	if ft.Key == tt.Key {
		if th.dirLen == 0 {
			if err := c.convertBucketSameKey(ft, fh.dirPtr, tt, th.dirPtr, nil, 0); err != nil {
				return nil, err
			}
			th.used = fh.used
			return unsafe.Pointer(th), nil
		}

		for ti := 0; ti < fh.dirLen; ti++ {
			ftab := fh.directoryAt(uintptr(ti))
			s := ftab.groups.lengthMask
			ttab := th.directoryAt(uintptr(ti))
			for bucket := uint64(0); bucket <= s; bucket++ {
				if err := c.convertBucketSameKey(ft, ftab.groups.group(ft, bucket).data, tt,
					ttab.groups.group(tt, bucket).data, nil, 0); err != nil {
					return nil, err
				}
			}
			ttab.used = ftab.used
			ttab.growthLeft = ftab.growthLeft
		}
		th.used = fh.used
	} else {
		if th.dirLen == 0 {
			if err := c.convertBucketDiffKey(ft, fh.dirPtr, tt, th); err != nil {
				return nil, err
			}
			return unsafe.Pointer(th), nil
		}

		for ti := 0; ti < fh.dirLen; ti++ {
			ftab := fh.directoryAt(uintptr(ti))
			s := ftab.groups.lengthMask
			for bucket := uint64(0); bucket <= s; bucket++ {
				if err := c.convertBucketDiffKey(ft, ftab.groups.group(ft, bucket).data, tt,
					th); err != nil {
					return nil, err
				}
			}
		}
	}
	return unsafe.Pointer(th), nil
}

func (c *tcConverter) convertSameKeyAndEle(fb, tb unsafe.Pointer, ft, tt *abi.MapType, fi, ti uintptr,
	needConvert bool) error {
	fk := unsafe.Pointer(uintptr(fb) + groupSlotsOffset + fi*ft.SlotSize)
	tk := unsafe.Pointer(uintptr(tb) + groupSlotsOffset + ti*tt.SlotSize)
	typedmemmove(tt.Key, tk, fk)

	fe := unsafe.Pointer(uintptr(fk) + ft.ElemOff)
	te := unsafe.Pointer(uintptr(tk) + tt.ElemOff)
	if c.context.data != nil {
		// The caller of convertSameKeyAndEle guarantees that tt and ft are equal.
		if !needConvert {
			typedmemmove(tt.Elem, te, fe)
		} else {
			*(*tcEface)(te) = c.context
			c.context = tcEface{}
		}
		return nil
	}
	ie := (*tcEface)(te)
	var err error
	ie.data, ie._type, err = tcConvertAny(c, fe, ft.Elem)
	return err
}
