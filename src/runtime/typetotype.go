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

var tcStructToStructCache unsafe.Pointer // map[[2]*_type]*tcStructToStructAddress.

const (
	tcTangoConv uint8 = iota
	tcDeepCopy
	tcCopier
	tcCopierUnexported
)

type tcStructToStructAddress struct {
	from     *tcStructFields
	to       *tcStructFields
	toToFrom [][2]int
}

//go:linkname tcTypeToType
func tcTypeToType(from, to interface{}, deepCopy bool, replaceFunc uint8) error {
	if from == nil || to == nil {
		return tcArgumentError(0)
	}
	fe := (*eface)(unsafe.Pointer(&from))
	te := (*eface)(unsafe.Pointer(&to))

	if te._type.Kind() != abi.Pointer {
		return errorString("TypeToType: the second argument should be a pointer")
	}

	c := tcGetInitializedConverter(deepCopy)
	c.replaceFunc = replaceFunc
	if tcGetDebug() {
		defer func() {
			if err := recover(); err != nil {
				c.reportTrace()
			}
		}()
	}

	var err error

	if tcIsDirectIface(te._type) {
		if tcIsDirectIface(fe._type) {
			err = c.typeToType(noescape(unsafe.Pointer(&fe.data)), unsafe.Pointer(&te.data), fe._type, te._type)
		} else {
			err = c.typeToType(fe.data, unsafe.Pointer(&te.data), fe._type, te._type)
		}
	} else {
		if !inheap(uintptr(te.data)) {
			te.data = tcNewObjectWithValue(te._type, te.data)
		}
		if tcIsDirectIface(fe._type) {
			err = c.typeToType(noescape(unsafe.Pointer(&fe.data)), te.data, fe._type, te._type)
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

type tcDeepCopyFn interface {
	DeepCopy() interface{}
}

func (c *tcConverter) typeToType(from, to unsafe.Pointer, ft, tt *_type) error {
	if c.replaceFunc == tcDeepCopy && ft.TFlag&abi.TFlagUncommon != 0 &&
		*(*uint16)(add(unsafe.Pointer(ft), tcMcountOffset[int(ft.Kind())])) != 0 {
		if it := getitab(tcDeepCopyType, ft, true); it != nil {
			return c.tcHandleDeepCopyFn(from, to, ft, tt, it)
		}
	}
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
		if c.replaceFunc == tcCopier {
			return c.otherToOther(from, to, ft, tt)
		}
		return tcUnsupportedConvertError(tcTypeName(ft))
	}
}

//go:linkname tcTypeToTypePointer runtime.tcTypeToTypePointer
func tcTypeToTypePointer(c *tcConverter, from, to unsafe.Pointer, ft, tt *_type) error {
	return c.typeToType(from, to, ft, tt)
}

func (c *tcConverter) otherToOther(from, to unsafe.Pointer, ft, tt *_type) error {
	switch tt.Kind() {
	case abi.Interface:
		return c.handleInterface(from, to, ft, tt)
	case abi.Pointer:
		tt, to = tcHandlePtr(tt, to)
		return c.typeToType(from, to, ft, tt)
	default:
		if ft != tt {
			return nil
		}
		typedmemmove(ft, to, from)
	}
	return nil
}

func (c *tcConverter) tcHandleDeepCopyFn(from unsafe.Pointer, to unsafe.Pointer, ft *_type, tt *_type, it *itab) error {
	var j tcDeepCopyFn
	ji := (*iface)((unsafe.Pointer)(&j))
	ji.tab = it
	if tcIsDirectIface(ft) {
		ji.data = tcDereference(from)
	} else {
		ji.data = from
	}
	if ji.data == nil {
		return nil
	}
	k := j.DeepCopy()
	ke := (*eface)((unsafe.Pointer)(&k))
	if tcIsDirectIface(ke._type) {
		from = noescape(unsafe.Pointer(&ke.data))
	} else {
		from = ke.data
	}
	// The 'c' here is created within the interface and there is no issue of concurrent reading and writing.
	// After calling the DeepCopy interface of the object,
	// a shallow copy should be performed and the result of DeepCopy should be assigned to 'to'.
	// Considering that the cases of implementing the DeepCopy interface are few,
	// a deep copy is used for copying here. During the copy process,
	// the'replaceFunc' status needs to be set to 'tcTangoConv' to prevent re-entering the logic of handling the
	// DeepCopy interface. At the same time, 'deepCopy' needs to be set to 'true' to copy all fields in the struct.
	// After the copy is completed, the status needs to be retraced.
	c.replaceFunc, c.deepCopy = tcTangoConv, true
	err := c.typeToType(from, to, ke._type, tt)
	c.replaceFunc, c.deepCopy = tcDeepCopy, false
	return err
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
	if f := tcLoadCache(tcStructToStructCache, ft, tt); f != nil {
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
	tcStoreCache(tcStructToStructCache, ft, tt, sts)
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
		if c.replaceFunc == tcDeepCopy && ft == tcTimeType {
			// time.Time is immutable, so a shallow copy can be used for copying.
			typedmemmove(ft, to, from)
			return nil
		}
		sts := c.cachedStructToStructIndex(ft, tt)
		c.pushTraceByType(ft)
		for i := range sts.toToFrom {
			ff := &sts.from.fields[sts.toToFrom[i][1]]
			c.pushTraceByString(ff.key)
			tf := &sts.to.fields[sts.toToFrom[i][0]]
			if c.replaceFunc == tcCopier && ff.ignoreCopier {
				c.popTrace()
				continue
			}
			if !c.deepCopy && (tcNeedSkip(ff) || tcNeedSkip(tf)) {
				if c.replaceFunc == tcDeepCopy && ff.structType == tcTimeType {
					tcHandleTimeField(from, to, ff, tf)
					c.popTrace()
					continue
				}
				if c.replaceFunc == tcTangoConv || !ff.export {
					c.popTrace()
					continue
				}
			}
			fp := tcLoadFieldAddr(from, ff)
			if fp == nil {
				c.popTrace()
				continue // embedded pointer field is nil
			}
			tp := tcLoadFieldAddrWithNewObject(to, tf)
			if !c.deepCopy && tcIsZeroValue(fp, ff.typ) {
				c.popTrace()
				continue
			}
			if c.replaceFunc == tcCopier {
				if !ff.export {
					if tcIsZeroValue(fp, ff.typ) {
						c.popTrace()
						continue
					}
					// The copier only performs a shallow copy for unexported fields.
					if ff.typ == tf.typ {
						typedmemmove(ff.typ, tp, fp)
					}
					c.popTrace()
					continue
				}
			}
			if err := c.typeToType(fp, tp, ff.typ, tf.typ); err != nil {
				return err
			}
			c.popTrace()
		}
		c.popTrace()
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

func tcHandleTimeField(from unsafe.Pointer, to unsafe.Pointer, ff *tcField, tf *tcField) {
	fp := tcLoadFieldAddr(from, ff)
	if fp == nil {
		return
	}
	tp := tcLoadFieldAddrWithNewObject(to, tf)
	// time.Time is immutable, so a shallow copy can be used for copying.
	typedmemmove(ff.typ, tp, fp)
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
			ts.len = 0
			ts.cap = 0
			if c.replaceFunc != tcCopier {
				ts.array = fs.array
			} else {
				ts.array = mallocgc(0, tet, true)
			}
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
		c.stackLevel++
		if c.stackLevel > tcMaxStackLevel {
			return tcCircularReference("")
		}
		for i := 0; i < l; i++ {
			fe := add(fs.array, uintptr(i)*fet.Size())
			te := add(ts.array, uintptr(i)*tet.Size())
			if err := c.typeToType(fe, te, fet, tet); err != nil {
				// If an error occurs, it will be propagated upward continuously,
				// so there is no need to correct the stackLevel.
				return err
			}
		}
		c.stackLevel--
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

const tcMaxStackLevel = 1000

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
		c.stackLevel++
		if c.stackLevel > tcMaxStackLevel {
			return tcCircularReference("")
		}
		to = tcDereferenceWithNewObject(to, tpt.Elem)
		err := c.typeToType(from, to, fpt.Elem, tpt.Elem)
		c.stackLevel--
		return err
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
func tcBucketToBucketDiff(c *tcConverter, ft, tt *abi.MapType, fh, th unsafe.Pointer) error

//go:linkname tcBucketToBucketSame
func tcBucketToBucketSame(c *tcConverter, ft, tt *abi.MapType, fh, th unsafe.Pointer) error

func (c *tcConverter) mapToMap(from, to unsafe.Pointer, ft, tt *_type) error {
	switch tt.Kind() {
	case abi.Map:
		fh := tcDereference(from)
		if fh == nil {
			if c.replaceFunc != tcCopier {
				*(**maps.Map)(to) = nil
			} else {
				*(**maps.Map)(to) = makemap((*abi.MapType)(unsafe.Pointer(tt)), 0, nil)
			}
			return nil
		}
		fm := (*abi.MapType)(unsafe.Pointer(ft))
		tm := (*abi.MapType)(unsafe.Pointer(tt))
		*(*unsafe.Pointer)(to) = tcAllocMap(fh, tm)
		if (*maps.Map)(fh).Used() == 0 {
			return nil
		}
		c.stackLevel++
		if c.stackLevel > tcMaxStackLevel {
			return tcCircularReference("")
		}
		if fm.Key == tm.Key {
			err := tcBucketToBucketSame(c, fm, tm, fh, tcDereference(to))
			if err != nil {
				return err
			}
		} else {
			err := tcBucketToBucketDiff(c, fm, tm, fh, tcDereference(to))
			if err != nil {
				return err
			}
		}
		c.stackLevel--
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
