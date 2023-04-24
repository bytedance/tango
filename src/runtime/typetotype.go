// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/abi"
	"internal/runtime/maps"
	"unsafe"
)

func tcIsDirectIface(t *_type) bool {
	return t.IsDirectIface()
}

var tcStructToStructCache tcCache // map[[2]*_type]*tcStructToStructAddress.

type tcStructToStructAddress struct {
	from     *tcStructFields
	to       *tcStructFields
	toToFrom [][2]int
}

//go:linkname tcTypeToType
func tcTypeToType(from, to interface{}, deepCopy bool) error {
	if from == nil || to == nil {
		return tcArgumentError(0)
	}
	fe := (*eface)(unsafe.Pointer(&from))
	te := (*eface)(unsafe.Pointer(&to))

	if te._type.Kind() != abi.Pointer {
		return errorString("TypeToType: the second argument should be a pointer")
	}

	c := tcGetConverter().(*tcConverter)
	c.deepCopy = deepCopy
	var err error
	if tcIsDirectIface(te._type) {
		if tcIsDirectIface(fe._type) {
			err = c.typeToType(unsafe.Pointer(&fe.data), unsafe.Pointer(&te.data), fe._type, te._type)
		} else {
			err = c.typeToType(fe.data, unsafe.Pointer(&te.data), fe._type, te._type)
		}
	} else {
		if !inheap(uintptr(te.data)) {
			te.data = tcNewObjectWithValue(te._type, te.data)
		}
		if tcIsDirectIface(fe._type) {
			err = c.typeToType(unsafe.Pointer(&fe.data), te.data, fe._type, te._type)
		} else {
			err = c.typeToType(fe.data, te.data, fe._type, te._type)
		}
	}
	tcPutConverter(c)
	if err != nil {
		return err
	}
	return nil
}

func (c *tcConverter) typeToType(from, to unsafe.Pointer, ft, tt *_type) error {
	kind := ft.Kind()
	if kind <= abi.Float64 {
		return c.baseToBase(from, to, ft, tt)
	}
	switch kind {
	case abi.String:
		return c.baseToBase(from, to, ft, tt)
	case abi.Interface:
		return c.interfaceToInterface(from, to, ft, tt)
	case abi.Struct:
		return c.structToStruct(from, to, ft, tt)
	case abi.Map:
		return c.mapToMap(from, to, ft, tt)
	case abi.Slice:
		return c.sliceToSlice(from, to, ft, tt)
	case abi.Array:
		return c.arrayToArray(from, to, ft, tt)
	case abi.Pointer:
		return c.ptrToPtr(from, to, ft, tt)
	default:
		return tcUnsupportedConvertError(tcTypeName(ft))
	}
}

//go:linkname tcTypeToTypePointer runtime.tcTypeToTypePointer
func tcTypeToTypePointer(c *tcConverter, from, to unsafe.Pointer, ft, tt *_type) error {
	return c.typeToType(from, to, ft, tt)
}

func tcHandlePtr(t *_type, p unsafe.Pointer) (*_type, unsafe.Pointer) {
	tpt := (*ptrtype)(unsafe.Pointer(t))
	return tpt.Elem, tcDereferenceWithNewObject(p, tpt.Elem)
}

func (c *tcConverter) handleInterface(from, to unsafe.Pointer, ft, tt *_type) error {
	tit := (*interfacetype)(unsafe.Pointer(tt))
	if len(tit.Methods) != 0 {
		ti := (*iface)(to)
		if ti.tab == nil {
			if ti.data != nil {
				return tcBadStateError(0)
			}
			ti.tab = getitab(tit, ft, true)
			if ti.tab == nil {
				return tcIllegalConvertError(tcTypeName(ft) + " to " + tcTypeName(tt))
			}
		}
		tt = ti.tab.Type
		if tcIsDirectIface(tt) {
			to = unsafe.Pointer(&ti.data)
		} else {
			if ti.data == nil || !inheap(uintptr(ti.data)) {
				ti.data = tcNewObjectWithValue(tt, ti.data)
			}
			to = ti.data
		}
	} else {
		te := (*eface)(to)
		if te._type == nil {
			if te.data != nil {
				return tcBadStateError(0)
			}
			te._type = ft
		}
		tt = te._type
		if tcIsDirectIface(tt) {
			to = unsafe.Pointer(&te.data)
		} else {
			// We can't take address of const literal directly,
			// but we can take address of const literal indirectly by interface.
			// We must ensure that the data pointer of the interface does not point to RODATA.
			if te.data == nil || !inheap(uintptr(te.data)) {
				te.data = tcNewObjectWithValue(te._type, te.data)
			}
			to = te.data
		}
	}
	return c.typeToType(from, to, ft, tt)
}

func tcNewObjectWithValue(typ *_type, p unsafe.Pointer) unsafe.Pointer {
	tmp := newobject(typ)
	if p != nil {
		typedmemmove(typ, tmp, p)
	}
	return tmp
}

func (c *tcConverter) baseToBase(from, to unsafe.Pointer, ft, tt *_type) error {
	tk := tt.Kind()
	switch tk {
	case abi.Interface:
		return c.handleInterface(from, to, ft, tt)
	case abi.Pointer:
		tt, to = tcHandlePtr(tt, to)
		return c.typeToType(from, to, ft, tt)
	case abi.String:
		if ft.Kind() != tk {
			return nil
		}
		typedmemmove(ft, to, from)
		return nil
	default:
		fk := ft.Kind()
		if fk == tk {
			memmove(to, from, ft.Size())
			return nil
		}
		if fk > abi.Float64 || tk > abi.Float64 {
			return nil
		}
		switch tk {
		case abi.Uint8:
			*(*uint8)(to) = tcGetNumber[uint8](from, fk)
		case abi.Uint16:
			*(*uint16)(to) = tcGetNumber[uint16](from, fk)
		case abi.Uint32:
			*(*uint32)(to) = tcGetNumber[uint32](from, fk)
		case abi.Uint64:
			*(*uint64)(to) = tcGetNumber[uint64](from, fk)
		case abi.Int:
			*(*int)(to) = tcGetNumber[int](from, fk)
		case abi.Int8:
			*(*int8)(to) = tcGetNumber[int8](from, fk)
		case abi.Int16:
			*(*int16)(to) = tcGetNumber[int16](from, fk)
		case abi.Int32:
			*(*int32)(to) = tcGetNumber[int32](from, fk)
		case abi.Int64:
			*(*int64)(to) = tcGetNumber[int64](from, fk)
		case abi.Uint:
			*(*uint)(to) = tcGetNumber[uint](from, fk)
		case abi.Uintptr:
			*(*uintptr)(to) = tcGetNumber[uintptr](from, fk)
		case abi.Float32:
			*(*float32)(to) = tcGetNumber[float32](from, fk)
		case abi.Float64:
			*(*float64)(to) = tcGetNumber[float64](from, fk)
		}
	}
	return nil
}

func tcGetNumber[T tcNumber](from unsafe.Pointer, fk abi.Kind) T {
	switch fk {
	case abi.Uint8:
		return T(*(*uint8)(from))
	case abi.Uint16:
		return T(*(*uint16)(from))
	case abi.Uint32:
		return T(*(*uint32)(from))
	case abi.Uint64:
		return T(*(*uint64)(from))
	case abi.Uint:
		return T(*(*uint)(from))
	case abi.Int:
		return T(*(*int)(from))
	case abi.Int8:
		return T(*(*int8)(from))
	case abi.Int16:
		return T(*(*int16)(from))
	case abi.Int32:
		return T(*(*int32)(from))
	case abi.Int64:
		return T(*(*int64)(from))
	case abi.Uintptr:
		return T(*(*uintptr)(from))
	case abi.Float32:
		return T(*(*float32)(from))
	case abi.Float64:
		return T(*(*float64)(from))
	}
	return 0
}

func (c *tcConverter) interfaceToInterface(from, to unsafe.Pointer, ft, tt *_type) error {
	switch tt.Kind() {
	case abi.Interface:
		fit := (*interfacetype)(unsafe.Pointer(ft))
		if len(fit.Methods) != 0 {
			fi := (*iface)(from)
			if fi.tab == nil {
				if fi.data != nil {
					return tcBadStateError(0)
				}
				return nil
			}
			if tcIsDirectIface(fi.tab.Type) {
				return c.handleInterface(unsafe.Pointer(&fi.data), to, fi.tab.Type, tt)
			} else {
				return c.handleInterface(fi.data, to, fi.tab.Type, tt)
			}
		} else {
			fe := (*eface)(from)
			if fe._type == nil {
				if fe.data != nil {
					return tcBadStateError(0)
				}
				return nil
			}
			if tcIsDirectIface(fe._type) {
				return c.handleInterface(unsafe.Pointer(&fe.data), to, fe._type, tt)
			} else {
				return c.handleInterface(fe.data, to, fe._type, tt)
			}
		}
	default:
		var err error
		from, ft, err = c.unpackInterface(from, ft)
		if err != nil {
			return err
		}
		if ft == nil {
			if from != nil {
				return tcBadStateError(0)
			}
			return nil
		}
		return c.typeToType(from, to, ft, tt)
	}
}

func (c *tcConverter) cachedStructToStructIndex(ft, tt *_type) *tcStructToStructAddress {
	if f := tcStructToStructCache.load([2]*_type{ft, tt}); f != nil {
		return f.(*tcStructToStructAddress)
	}
	var sts = new(tcStructToStructAddress)
	ffmc := c.getCachedStructFields(ft)
	tfmc := c.getCachedStructFields(tt)
	sts.from = ffmc
	sts.to = tfmc
	if ft != tt {
		for i := range tfmc.fields {
			tf := &tfmc.fields[i]
			if idx, ok := ffmc.nameIndex[tf.key]; ok {
				sts.toToFrom = append(sts.toToFrom, [2]int{i, idx})
			}
		}
	} else {
		for i := range tfmc.fields {
			sts.toToFrom = append(sts.toToFrom, [2]int{i, i})
		}
	}
	tcStructToStructCache.store([2]*_type{ft, tt}, sts)
	return sts
}

// tcDereferenceWithNewObject makes sure the element returned after dereferencing is not nil.
//
//go:linkname tcDereferenceWithNewObject
func tcDereferenceWithNewObject(p unsafe.Pointer, typ *_type) unsafe.Pointer {
	ele := tcDereference(p)
	if ele == nil || !inheap(uintptr(ele)) {
		*(*unsafe.Pointer)(p) = tcNewObjectWithValue(typ, ele)
	}
	return tcDereference(p)
}

func tcLoadFieldAddrWithNewObject(p unsafe.Pointer, f *tcField) unsafe.Pointer {
	p = add(p, f.offset[0])
	for i := 1; i < len(f.offset); i++ {
		p = tcDereferenceWithNewObject(p, f.structType)
		p = add(p, f.offset[i])
	}
	return p
}

func tcIsZeroValue(p unsafe.Pointer, t *_type) bool {
	kind := t.Kind()
	if kind <= abi.Float64 {
		return memequal(p, unsafe.Pointer(&zeroVal), t.Size())
	} else if abi.Map <= kind && kind <= abi.String {
		return tcDereference(p) == nil
	} else if kind == abi.Array {
		for i := uintptr(0); i < (*arraytype)(unsafe.Pointer(t)).Len; i++ {
			if !tcIsZeroValue(add(p, i*(*arraytype)(unsafe.Pointer(t)).Elem.Size()),
				(*arraytype)(unsafe.Pointer(t)).Elem) {
				return false
			}
		}
	}
	return false
}

func (c *tcConverter) structToStruct(from, to unsafe.Pointer, ft, tt *_type) error {
	switch tt.Kind() {
	case abi.Struct:
		sts := c.cachedStructToStructIndex(ft, tt)
		for i := range sts.toToFrom {
			ff := &sts.from.fields[sts.toToFrom[i][1]]
			tf := &sts.to.fields[sts.toToFrom[i][0]]
			if !c.deepCopy && (tcNeedSkip(ff) || tcNeedSkip(tf)) {
				continue
			}
			fp := tcLoadFieldAddr(from, ff)
			if fp == nil {
				continue // embedded pointer field is nil
			}
			tp := tcLoadFieldAddrWithNewObject(to, tf)
			if !c.deepCopy && tcIsZeroValue(fp, ff.typ) {
				continue
			}
			if err := c.typeToType(fp, tp, ff.typ, tf.typ); err != nil {
				return err
			}
		}
	case abi.Interface:
		return c.handleInterface(from, to, ft, tt)
	case abi.Pointer:
		tt, to = tcHandlePtr(tt, to)
		return c.typeToType(from, to, ft, tt)
	default:
		// if we can't handle convert. just ignore it. no return err.
		return nil
	}
	return nil
}

func (c *tcConverter) sliceToSlice(from, to unsafe.Pointer, ft, tt *_type) error {
	switch tt.Kind() {
	case abi.Slice:
		fst := (*slicetype)(unsafe.Pointer(ft))
		tst := (*slicetype)(unsafe.Pointer(tt))
		fet := fst.Elem
		tet := tst.Elem
		fs := (*slice)(from)
		// slice may be nil,
		// we should make interface to be nil.
		ts := (*slice)(to)
		if fs.array == nil {
			ts.array = fs.array
			ts.len = 0
			ts.cap = 0
			return nil
		}
		// we assume ts.len == 0 means needing allocation.
		if ts.len == 0 || !inheap(uintptr(ts.array)) {
			ts.len = fs.len
			ts.cap = fs.len
			ts.array = mallocgc(uintptr(ts.len)*tet.Size(), tet, true)
		}
		l := fs.len
		if l > ts.len {
			l = ts.len
		}
		for i := 0; i < l; i++ {
			fe := add(fs.array, uintptr(i)*fet.Size())
			te := add(ts.array, uintptr(i)*tet.Size())
			if err := c.typeToType(fe, te, fet, tet); err != nil {
				return err
			}
		}
	case abi.Interface:
		return c.handleInterface(from, to, ft, tt)
	case abi.Pointer:
		tt, to = tcHandlePtr(tt, to)
		return c.typeToType(from, to, ft, tt)
	default:
		// if we can't handle convert. just ignore it. no return err.
		return nil
	}
	return nil
}

func (c *tcConverter) arrayToArray(from, to unsafe.Pointer, ft, tt *_type) error {
	switch tt.Kind() {
	case abi.Array:
		fat := (*arraytype)(unsafe.Pointer(ft))
		tat := (*arraytype)(unsafe.Pointer(tt))
		fet := fat.Elem
		tet := tat.Elem
		l := fat.Len
		if l > tat.Len {
			l = tat.Len
		}
		for i := uintptr(0); i < l; i++ {
			fe := add(from, i*fet.Size())
			te := add(to, i*tet.Size())
			if err := c.typeToType(fe, te, fet, tet); err != nil {
				return err
			}
		}
	case abi.Interface:
		return c.handleInterface(from, to, ft, tt)
	case abi.Pointer:
		tt, to = tcHandlePtr(tt, to)
		return c.typeToType(from, to, ft, tt)
	default:
		// if we can't handle convert. just ignore it. no return err.
		return nil
	}
	return nil
}

func (c *tcConverter) ptrToPtr(from, to unsafe.Pointer, ft, tt *_type) error {
	switch tt.Kind() {
	case abi.Pointer:
		fpt := (*ptrtype)(unsafe.Pointer(ft))
		tpt := (*ptrtype)(unsafe.Pointer(tt))
		from = tcDereference(from)
		if from == nil {
			*(*unsafe.Pointer)(to) = nil
			return nil
		}
		to = tcDereferenceWithNewObject(to, tpt.Elem)
		return c.typeToType(from, to, fpt.Elem, tpt.Elem)
	default:
		// we shouldn't change input.
		if *(*unsafe.Pointer)(from) == nil {
			return nil
		}
		ft, from = tcHandlePtr(ft, from)
		return c.typeToType(from, to, ft, tt)
	}
}

//go:linkname tcBucketToBucketDiff
func tcBucketToBucketDiff(c *tcConverter, ft, tt *abi.MapType, fh, th unsafe.Pointer)

//go:linkname tcBucketToBucketSame
func tcBucketToBucketSame(c *tcConverter, ft, tt *abi.MapType, fh, th unsafe.Pointer)

func (c *tcConverter) mapToMap(from, to unsafe.Pointer, ft, tt *_type) error {
	switch tt.Kind() {
	case abi.Map:
		fh := tcDereference(from)
		if fh == nil {
			*(*unsafe.Pointer)(to) = nil
			return nil
		}
		fm := (*abi.MapType)(unsafe.Pointer(ft))
		tm := (*abi.MapType)(unsafe.Pointer(tt))
		*(*unsafe.Pointer)(to) = tcAllocMap(fh, tm)
		if (*maps.Map)(fh).Used() == 0 {
			return nil
		}
		if fm.Key == tm.Key {
			tcBucketToBucketSame(c, fm, tm, fh, tcDereference(to))
		} else {
			tcBucketToBucketDiff(c, fm, tm, fh, tcDereference(to))
		}
	case abi.Interface:
		return c.handleInterface(from, to, ft, tt)
	case abi.Pointer:
		tt, to = tcHandlePtr(tt, to)
		return c.typeToType(from, to, ft, tt)
	default:
		// if we can't handle convert. just ignore it. Don't return err.
		return nil
	}
	return nil
}
