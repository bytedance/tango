// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/abi"
	"internal/runtime/maps"
	"unsafe"
)

const (
	tcToOrig = iota
	tcToFloat64
	tcToJsonNumber
)

var (
	tcReflectTypeItab     *itab
	tcEfaceSliceType      *_type
	tcFloat64Type         *_type
	tcJsonNumberType      *_type
	tcStringType          *_type // runtime.stringType is not equal to this.
	tcTimeType            *_type
	tcBoolType            *_type
	tcInterfaceType       *_type
	tcStringEfaceMapType  *_type
	tcJsonType            *interfacetype
	tcTextType            *interfacetype
	tcDeepCopyType        *interfacetype
	tcStructFieldsCache   unsafe.Pointer // map[*type]*tcStructFields.
	tcEscapeFloatSci      func(f float64, format byte, bitSize int) unsafe.Pointer
	tcParseFloat          func(s string, bitSize int) (float64, error)
	tcExtractStructFields func(t iface) ([]bool, [][]int, []string, []bool, []iface, []bool, []bool)
	tcGetConverter        func() any
	tcPutConverter        func(any)
	tcParseInterface      func(b []byte, useNumber bool) any
	tcMarshalJson         func(i iface) ([]byte, error)
	tcMarshalText         func(i iface) ([]byte, error)
	tcInitCache           func(m unsafe.Pointer)
	tcLoadCache           func(m unsafe.Pointer, t1, t2 *_type) any
	tcStoreCache          func(m unsafe.Pointer, t1, t2 *_type, v any)
)

//go:linkname tcInitValues
func tcInitValues() {
	tcBaseLen = int(tcStringType.Size()) * 10
	tcLimitLen = tcBaseLen << 8
	tcTrueVal = true
	tcFalseVal = false
	tcTrueStr = "true"
	tcFalseStr = "false"
	smallsString := []byte{
		'0', '0', '0', '1', '0', '2', '0', '3', '0', '4', '0', '5', '0', '6', '0', '7', '0', '8', '0', '9',
		'1', '0', '1', '1', '1', '2', '1', '3', '1', '4', '1', '5', '1', '6', '1', '7', '1', '8', '1', '9',
		'2', '0', '2', '1', '2', '2', '2', '3', '2', '4', '2', '5', '2', '6', '2', '7', '2', '8', '2', '9',
		'3', '0', '3', '1', '3', '2', '3', '3', '3', '4', '3', '5', '3', '6', '3', '7', '3', '8', '3', '9',
		'4', '0', '4', '1', '4', '2', '4', '3', '4', '4', '4', '5', '4', '6', '4', '7', '4', '8', '4', '9',
		'5', '0', '5', '1', '5', '2', '5', '3', '5', '4', '5', '5', '5', '6', '5', '7', '5', '8', '5', '9',
		'6', '0', '6', '1', '6', '2', '6', '3', '6', '4', '6', '5', '6', '6', '6', '7', '6', '8', '6', '9',
		'7', '0', '7', '1', '7', '2', '7', '3', '7', '4', '7', '5', '7', '6', '7', '7', '7', '8', '7', '9',
		'8', '0', '8', '1', '8', '2', '8', '3', '8', '4', '8', '5', '8', '6', '8', '7', '8', '8', '8', '9',
		'9', '0', '9', '1', '9', '2', '9', '3', '9', '4', '9', '5', '9', '6', '9', '7', '9', '8', '9', '9'}
	tcSmallsString = *(*unsafe.Pointer)((unsafe.Pointer)(&smallsString))
	digits := []byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}
	tcDigits = *(*unsafe.Pointer)((unsafe.Pointer)(&digits))
	tcNum = []string{"first", "second"}
	tcInitMcountOffset()
	tcInitBasicTypes()
	tcInitCache(unsafe.Pointer(&tcStructFieldsCache))
	tcInitCache(unsafe.Pointer(&tcStructToStructCache))
}

func tcInitMcountOffset() {
	for k := abi.Bool; k <= abi.UnsafePointer; k++ {
		switch k {
		case abi.Bool, abi.Int8, abi.Int16, abi.Int32, abi.Int64, abi.Int,
			abi.Uint8, abi.Uint16, abi.Uint32, abi.Uint64, abi.Uint, abi.Uintptr,
			abi.Float32, abi.Float64, abi.Complex64, abi.Complex128,
			abi.String, abi.UnsafePointer:
			tcMcountOffset[k] = unsafe.Sizeof(_type{}) + unsafe.Sizeof(nameOff(0))
		case abi.Array:
			tcMcountOffset[k] = unsafe.Sizeof(arraytype{}) + unsafe.Sizeof(nameOff(0))
		case abi.Chan:
			tcMcountOffset[k] = unsafe.Sizeof(chantype{}) + unsafe.Sizeof(nameOff(0))
		case abi.Func:
			tcMcountOffset[k] = unsafe.Sizeof(functype{}) + unsafe.Sizeof(nameOff(0))
		case abi.Interface:
			tcMcountOffset[k] = unsafe.Sizeof(interfacetype{}) + unsafe.Sizeof(nameOff(0))
		case abi.Map:
			tcMcountOffset[k] = unsafe.Sizeof(abi.MapType{}) + unsafe.Sizeof(nameOff(0))
		case abi.Pointer:
			tcMcountOffset[k] = unsafe.Sizeof(ptrtype{}) + unsafe.Sizeof(nameOff(0))
		case abi.Slice:
			tcMcountOffset[k] = unsafe.Sizeof(slicetype{}) + unsafe.Sizeof(nameOff(0))
		case abi.Struct:
			tcMcountOffset[k] = unsafe.Sizeof(structtype{}) + unsafe.Sizeof(nameOff(0))
		default:
			panic("should not reach here")
		}
	}
}

func tcInitBasicTypes() {
	for k := abi.Bool; k <= abi.Float64; k++ {
		switch k {
		case abi.Bool:
			tcBasicTypes[k] = tcBoolType
		case abi.Int8:
			tcBasicTypes[k] = tcTypeOf(int8(0))
		case abi.Int16:
			tcBasicTypes[k] = tcTypeOf(int16(0))
		case abi.Int32:
			tcBasicTypes[k] = tcTypeOf(int32(0))
		case abi.Int64:
			tcBasicTypes[k] = tcTypeOf(int64(0))
		case abi.Int:
			tcBasicTypes[k] = tcTypeOf(0)
		case abi.Uint8:
			tcBasicTypes[k] = tcTypeOf(uint8(0))
		case abi.Uint16:
			tcBasicTypes[k] = tcTypeOf(uint16(0))
		case abi.Uint32:
			tcBasicTypes[k] = tcTypeOf(uint32(0))
		case abi.Uint64:
			tcBasicTypes[k] = tcTypeOf(uint64(0))
		case abi.Uint:
			tcBasicTypes[k] = tcTypeOf(uint(0))
		case abi.Uintptr:
			tcBasicTypes[k] = tcTypeOf(uintptr(0))
		case abi.Float32:
			tcBasicTypes[k] = tcTypeOf(float32(0))
		case abi.Float64:
			tcBasicTypes[k] = tcFloat64Type
		default:
			panic("should not reach here")
		}
	}
}

//go:linkname tcInitTypes
func tcInitTypes(jsonNumberReflectType, bigIntTextMarshal, bigIntJsonMarshaler iface, time interface{}) {
	tcReflectTypeItab = jsonNumberReflectType.tab
	tcJsonNumberType = (*_type)(jsonNumberReflectType.data)
	tcStringType = tcTypeOf("")
	tcEfaceSliceType = tcTypeOf([]interface{}{})
	tcFloat64Type = tcTypeOf(float64(0))
	tcBoolType = tcTypeOf(true)
	tcStringEfaceMapType = tcTypeOf(map[string]interface{}{})
	tcInterfaceType = tcTypeOf(eface{})
	tcTimeType = tcTypeOf(time)
	tcTextType = bigIntTextMarshal.tab.Inter
	tcJsonType = bigIntJsonMarshaler.tab.Inter
	tcDeepCopyType = tcGetInterfaceType((*tcDeepCopyFn)(nil))
}

func tcGetInterfaceType(dpFn interface{}) *interfacetype {
	return (*interfacetype)((unsafe.Pointer)((*ptrtype)((unsafe.Pointer)((*eface)((unsafe.Pointer)(&dpFn))._type)).Elem))
}

//go:linkname tcInitFuncs
func tcInitFuncs(escapeFloatSci func(f float64, format byte, bitSize int) unsafe.Pointer, getConverter func() any,
	putConverter func(any), parseFloat func(s string, bitSize int) (float64, error),
	extractStructFields func(t iface) ([]bool, [][]int, []string, []bool, []iface, []bool, []bool),
	parseInterface func(b []byte, useNumber bool) any,
	marshalJson func(i iface) ([]byte, error), marshalText func(i iface) ([]byte, error),
	initCache func(m unsafe.Pointer), loadCache func(m unsafe.Pointer, t1, t2 *_type) any,
	storeCache func(m unsafe.Pointer, t1, t2 *_type, v any)) {
	tcEscapeFloatSci = escapeFloatSci
	tcGetConverter = getConverter
	tcPutConverter = putConverter
	tcParseFloat = parseFloat
	tcExtractStructFields = extractStructFields
	tcParseInterface = parseInterface
	tcMarshalJson = marshalJson
	tcMarshalText = marshalText
	tcInitCache = initCache
	tcLoadCache = loadCache
	tcStoreCache = storeCache
}

type tcStructFields struct {
	fields    []tcField
	nameIndex map[string]int
	proto     unsafe.Pointer
}

type tcField struct {
	omitEmpty    bool
	export       bool
	ignoreJson   bool
	ignoreCopier bool
	mustDelete   bool
	typ          *_type
	structType   *_type    // structType indicates which struct type a field belongs to.
	offset       []uintptr // offset within the type.
	key          string
	hash         uintptr
	index        uintptr
	bucket       uintptr
	table        uintptr
	tophash      uint8
}

type tcConverter struct {
	numberKind  uint8
	deepCopy    bool
	context     eface
	ssBuffer    tcBuffer[stringStruct]
	bBuffer     tcBuffer[byte]
	replaceFunc uint8
	stackLevel  uint32
}

type tcBuffer[T any] struct {
	b    []T
	used int
}

func (buffer *tcBuffer[T]) getElement(num int) (res *T) {
	s := (*slice)(unsafe.Pointer(&buffer.b))
	if buffer.used+num < s.len {
		res = &buffer.b[buffer.used]
		buffer.used += num
		return
	}
	newLen := 2 * s.len
	if newLen*int(unsafe.Sizeof(*res)) >= tcLimitLen {
		newLen = s.len
	}
	buffer.b = make([]T, newLen)
	buffer.used = num
	return &buffer.b[0]
}

var (
	tcBaseLen  int
	tcLimitLen int
)

//go:linkname tcNewConverter
func tcNewConverter() any {
	c := new(tcConverter)
	c.ssBuffer.b = make([]stringStruct, tcBaseLen/int(tcStringType.Size()))
	c.bBuffer.b = make([]byte, tcBaseLen)
	return c
}

//go:linkname tcToStringMap
func tcToStringMap(from interface{}, preferredNumberKind iface, deepCopy bool) (map[string]interface{}, error) {
	if from == nil {
		return nil, tcArgumentError(0)
	}

	if data, typ := tcDereferencePtr(from); (typ.Kind() != abi.Struct) && (typ.Kind() != abi.Map) {
		return nil, tcArgumentTypeError(0)
	} else if data == nil {
		return nil, nil
	}
	c := tcGetInitializedConverter(deepCopy)
	switch (*_type)(preferredNumberKind.data) {
	case tcInterfaceType, nil:
		c.numberKind = tcToOrig
	case tcJsonNumberType:
		c.numberKind = tcToJsonNumber
	case tcFloat64Type:
		c.numberKind = tcToFloat64
	default:
		return nil, tcUnsupportedConvertError(tcTypeName((*_type)(preferredNumberKind.data)))
	}
	var (
		ie  eface
		err error
	)

	// Sometimes a type does not implement json.Marshaler itself, but its pointer type implements json.Marshaler,
	// such as math/big.Int. At this time, when a type is a pointer type, we should pass the type itself to convertAny
	// instead of the dereferenced type.
	if tcIsDirectIface((*eface)(unsafe.Pointer(&from))._type) {
		ie.data, ie._type, err = c.convertAny(unsafe.Pointer(&(*eface)(noescape(unsafe.Pointer(&from))).data), (*eface)(noescape(unsafe.Pointer(&from)))._type)
	} else {
		ie.data, ie._type, err = c.convertAny((*eface)(noescape(unsafe.Pointer(&from))).data, (*eface)(noescape(unsafe.Pointer(&from)))._type)
	}
	tcPutConverter(c)
	if err != nil {
		return nil, err
	}
	if tm, ok := tcPackEface(ie).(map[string]interface{}); !ok {
		return nil, errorString("struct converting to map[string]interface{} failed")
	} else {
		return tm, nil
	}
}

func tcGetInitializedConverter(deepCopy bool) *tcConverter {
	c := tcGetConverter().(*tcConverter)
	c.deepCopy = deepCopy
	c.stackLevel = 0
	return c
}

func tcPackReflectType(t *_type) (res iface) {
	res.data = unsafe.Pointer(t)
	res.tab = tcReflectTypeItab
	return
}

func tcDereferencePtr(to interface{}) (ptr unsafe.Pointer, typ *_type) {
	tm := (*eface)(unsafe.Pointer(&to))
	ptr = tm.data
	typ = tm._type
	for typ.Kind() == abi.Pointer {
		pt := (*ptrtype)(unsafe.Pointer(typ))
		if tcIsDirectIface(pt.Elem) {
			ptr = *((*unsafe.Pointer)(ptr))
		}
		typ = pt.Elem
	}
	return
}

func tcPackEface(e eface) interface{} {
	var i interface{}
	ie := (*eface)(unsafe.Pointer(&i))
	ie.data = e.data
	ie._type = e._type
	return i
}

func (c *tcConverter) unpackIface(p unsafe.Pointer) (unsafe.Pointer, *_type, error) {
	var i = (*iface)(p)
	if i.tab == nil {
		if i.data != nil {
			return nil, nil, tcBadStateError(0)
		}
		return nil, nil, nil
	}
	if i.tab.Inter == tcJsonType {
		return c.parseInterface(*i, tcMarshalJson)
	}
	if i.tab.Inter == tcTextType {
		return c.parseInterface(*i, tcMarshalText)
	}
	if tcIsDirectIface(i.tab.Type) {
		return unsafe.Pointer(&i.data), i.tab.Type, nil
	}
	return i.data, i.tab.Type, nil
}

func (c *tcConverter) parseInterface(i iface, marshaler func(i iface) ([]byte, error)) (unsafe.Pointer, *_type, error) {
	b, err := marshaler(i)
	if err != nil {
		return nil, nil, err
	}
	p := tcParseInterface(b, c.numberKind == tcToJsonNumber)
	return (*eface)(unsafe.Pointer(&p)).data, (*eface)(unsafe.Pointer(&p))._type, nil
}

func tcDereference(p unsafe.Pointer) unsafe.Pointer {
	return *(*unsafe.Pointer)(p)
}

func tcUnpackEface(p unsafe.Pointer) (unsafe.Pointer, *_type, error) {
	var e = (*eface)(p)
	if e._type == nil {
		if e.data != nil {
			return nil, nil, tcBadStateError(0)
		}
		return nil, nil, nil
	}
	if tcIsDirectIface(e._type) {
		return unsafe.Pointer(&e.data), e._type, nil
	}
	return e.data, e._type, nil
}

func (c *tcConverter) unpackInterface(p unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	it := (*interfacetype)(unsafe.Pointer(t))
	if len(it.Methods) != 0 {
		return c.unpackIface(p)
	}
	return tcUnpackEface(p)
}

func tcIndexToOffset(t *_type, index []int) ([]uintptr, *_type) {
	offset := make([]uintptr, 0, len(index))
	o := uintptr(0)
	for _, i := range index {
		for t.Kind() == abi.Pointer {
			offset = append(offset, o)
			o = 0
			t = (*ptrtype)(unsafe.Pointer(t)).Elem
		}
		f := (*structtype)(unsafe.Pointer(t)).Fields[i]
		o += f.Offset
		t = f.Typ
	}
	offset = append(offset, o)
	return offset, t
}

func (c *tcConverter) getCachedStructFields(tt *_type) *tcStructFields {
	if f := tcLoadCache(tcStructFieldsCache, tt, nil); f != nil {
		return f.(*tcStructFields)
	}
	t := tcPackReflectType(tt)
	sf := c.typeFastMapConvFields(t)
	tcStoreCache(tcStructFieldsCache, tt, nil, sf)
	return sf
}

func (c *tcConverter) typeFastMapConvFields(t iface) *tcStructFields {
	omitemptyRes, indexRes, nameRes, exportRes, belongStructType, ignoreJsonRes,
		ignoreCopierRes := tcExtractStructFields(t)
	var fmcStruct = new(tcStructFields)
	fmcStruct.fields = make([]tcField, len(omitemptyRes))
	fmcStruct.nameIndex = make(map[string]int, len(omitemptyRes))
	m := make(map[string]interface{}, len(omitemptyRes))
	fmcStruct.proto = *(*unsafe.Pointer)((unsafe.Pointer)(&m))
	for i := range omitemptyRes {
		fmcStruct.fields[i].omitEmpty = omitemptyRes[i]
		fmcStruct.fields[i].export = exportRes[i]
		var seed = tcGetSeed(fmcStruct.proto)
		fmcStruct.fields[i].ignoreJson = ignoreJsonRes[i]
		fmcStruct.fields[i].ignoreCopier = ignoreCopierRes[i]
		fmcStruct.fields[i].key = nameRes[i]
		fmcStruct.fields[i].structType = (*_type)(belongStructType[i].data)
		// First: record hash value.
		fmcStruct.fields[i].hash = stringHash(fmcStruct.fields[i].key, seed)
		// Second: convert index to offset.
		fmcStruct.fields[i].offset, fmcStruct.fields[i].typ = tcIndexToOffset((*_type)(t.data), indexRes[i])
		// Thirdly: map index and field.
		if _, ok := fmcStruct.nameIndex[fmcStruct.fields[i].key]; !ok {
			// Considering that anonymous nested classes may have fields with the same name,
			// since the semantics of golang is to preferentially match fields with a shallower nesting level,
			// in this case fmcStruct.fields is obtained by the bfs algorithm, so a larger field index means a deeper nesting level.
			// Therefore, when we have a key with the same name in the map, we should not replace it with a deeper value.
			fmcStruct.nameIndex[fmcStruct.fields[i].key] = i
		}
	}
	tcBuildProtoAndIndex(m, fmcStruct)
	return fmcStruct
}

func tcBuildProtoAndIndex(m map[string]interface{}, fmcStruct *tcStructFields) {
	// 1. Build proto.
	for i := range fmcStruct.fields {
		f := &fmcStruct.fields[i]
		if tcNeedSkip(f) {
			continue
		}
		m[f.key] = nil // put key into proto but without a valid value
	}
	// 2. Collect hash and index.
	// When converting a struct to map[string]interface{},
	// keys are same in from-map and to-map,
	// which means they will hash into the same bucket and the same ceil.
	// We can cache the location.
	// See runtime/map.go "mapassign" for more detailed.
	for i := range fmcStruct.fields {
		// Because the unexported fields are ignored when building the prototype above, we also need to ignore the unexported fields here.
		// Otherwise, the map[string]interface{} constructed from the proto and index information will have gaps(there may be emptyRest between the two TopHashes).
		// Because the traversal logic of runtime/map.go will stop when it meets emptyRest, some elements in the map cannot be traversed.
		// e.g. "mapaccess1" in runtime/map.go.
		f := &fmcStruct.fields[i]
		if tcNeedSkip(f) {
			continue
		}
		f.table, f.bucket, f.index, f.tophash, f.mustDelete = tcGetBucketOffset(fmcStruct.proto,
			(*abi.MapType)(unsafe.Pointer(tcStringEfaceMapType)),
			f.key, f.hash)
	}
}

var (
	tcTrueVal  bool
	tcFalseVal bool
)

func (c *tcConverter) convertBool(from unsafe.Pointer) (unsafe.Pointer, *_type, error) {
	if !c.deepCopy {
		return from, tcBoolType, nil
	}
	b := *(*bool)(from)
	if b {
		return unsafe.Pointer(&tcTrueVal), tcBoolType, nil
	} else {
		return unsafe.Pointer(&tcFalseVal), tcBoolType, nil
	}
}

func (c *tcConverter) convertString(from unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	// t may be json.Number.
	if !c.deepCopy {
		if t == tcJsonNumberType {
			return from, t, nil
		}
		return from, tcStringType, nil
	}
	var s = c.ssBuffer.getElement(1)
	*s = *(*stringStruct)(from)
	if t == tcJsonNumberType {
		return unsafe.Pointer(s), t, nil
	}
	return unsafe.Pointer(s), tcStringType, nil
}

func (c *tcConverter) convertMarshaler(ptr unsafe.Pointer, t *_type, it *itab, marshaler func(i iface) ([]byte, error)) (unsafe.Pointer, *_type, error) {
	var j = iface{tab: it}
	if tcIsDirectIface(t) {
		j.data = tcDereference(ptr)
	} else {
		j.data = ptr
	}
	if j.data == nil {
		return nil, nil, nil
	}
	return c.parseInterface(j, marshaler)
}

var tcMcountOffset [27]uintptr

// convertAny does the following type conversion:
// X may be 8, 16, 32, 64.
// T may be bool, intX, uintX, floatX, string, struct, map[bool/intX/uintX/uintptr/string]T, []T, [N]T, *T, interface{}.
// 1.bool, intX, uintX, floatX, string -> interface{}
// 2.struct -> map[string]interface{}
// 3.map[bool/intX/uintX/uintptr/string]T -> map[string]interface{}
// 4.[]T, [N]T -> []interface{}
// 5.*T, interface{} -> dereference or unpack, and goto 1,2,3,4,5
// Whether data is value type or reference type, ptr is pointer to data.
func (c *tcConverter) convertAny(ptr unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	if t.TFlag&abi.TFlagUncommon != 0 &&
		*(*uint16)(add(unsafe.Pointer(t), tcMcountOffset[int(t.Kind())])) != 0 {
		if t == tcJsonNumberType {
			goto convert
		}
		if it := getitab(tcJsonType, t, true); it != nil {
			return c.convertMarshaler(ptr, t, it, tcMarshalJson)
		}
		if it := getitab(tcTextType, t, true); it != nil {
			return c.convertMarshaler(ptr, t, it, tcMarshalText)
		}
	}
convert:
	switch t.Kind() {
	case abi.Bool:
		return c.convertBool(ptr)
	case abi.Int:
		return tcConvertNumber[int](c, ptr, t)
	case abi.Uint:
		return tcConvertNumber[uint](c, ptr, t)
	case abi.Uintptr:
		return tcConvertNumber[uintptr](c, ptr, t)
	case abi.Int8:
		return tcConvertNumber[int8](c, ptr, t)
	case abi.Uint8:
		return tcConvertNumber[uint8](c, ptr, t)
	case abi.Int16:
		return tcConvertNumber[int16](c, ptr, t)
	case abi.Uint16:
		return tcConvertNumber[uint16](c, ptr, t)
	case abi.Int32:
		return tcConvertNumber[int32](c, ptr, t)
	case abi.Uint32:
		return tcConvertNumber[uint32](c, ptr, t)
	case abi.Int64:
		return tcConvertNumber[int64](c, ptr, t)
	case abi.Uint64:
		return tcConvertNumber[uint64](c, ptr, t)
	case abi.Float32:
		return tcConvertNumber[float32](c, ptr, t)
	case abi.Float64:
		return tcConvertNumber[float64](c, ptr, t)
	case abi.String:
		return c.convertString(ptr, t)
	case abi.Interface:
		return c.convertInterface(ptr, t)
	case abi.Struct:
		return c.convertStruct(ptr, t)
	case abi.Map:
		return c.convertMap(ptr, t)
	case abi.Slice:
		return c.convertSlice(ptr, t)
	case abi.Array:
		return c.convertArray(ptr, t)
	case abi.Pointer:
		return c.convertPtr(ptr, t)
	default:
		return nil, nil, tcUnsupportedTypeError(tcTypeName(t))
	}
}

//go:linkname tcConvertAny
func tcConvertAny(c *tcConverter, p unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	return c.convertAny(p, t)
}

func (c *tcConverter) convertStruct(p unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	if p == nil {
		return nil, nil, nil
	}
	fmcStruct := c.getCachedStructFields(t)
	tt := (*abi.MapType)(unsafe.Pointer(tcStringEfaceMapType))
	th := tcAllocMap(fmcStruct.proto, tt)
	tcHandleUsed(th, fmcStruct.proto)

	for i := range fmcStruct.fields {
		f := &fmcStruct.fields[i]
		if tcNeedSkip(f) {
			continue
		}
		index, ttab, tb := tcHandleOverflow(th, f.table, f.bucket, f.index, tt)
		fp := tcLoadFieldAddr(p, f)
		if fp == nil || (f.omitEmpty && tcIsEmptyValue(fp, f.typ)) {
			tcHandleEmpty(tb, index, th, ttab)
			continue
		}
		tcSetHash(tb, index, f.tophash)
		// k is *string.
		*(*string)(add(tb, tcGetKeyOffset(index, tt))) = f.key
		// e is *eface, it can't be indirectelem.
		e := (*eface)(add(tb, tcGetElemOffset(index, tt)))
		var err error
		if e.data, e._type, err = c.convertAny(fp, f.typ); err != nil {
			return nil, nil, err
		}
	}

	return th, tcStringEfaceMapType, nil
}

func (c *tcConverter) convertPtr(p unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	pt := (*ptrtype)(unsafe.Pointer(t))
	p = tcDereference(p)
	if p == nil {
		return nil, nil, nil
	}
	return c.convertAny(p, pt.Elem)
}

func tcLoadFieldAddr(p unsafe.Pointer, f *tcField) unsafe.Pointer {
	p = add(p, f.offset[0])
	for i := 1; i < len(f.offset); i++ {
		p = tcDereference(p)
		if p == nil {
			return nil
		}
		p = add(p, f.offset[i])
	}
	return p
}

func tcTypeName(ft *_type) string {
	return toRType(ft).name()
}

func tcNeedSkip(f *tcField) bool {
	return !f.export || f.ignoreJson
}

// p is the start of the array.
func (c *tcConverter) convertArray(p unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	at := (*arraytype)(unsafe.Pointer(t))
	l := at.Len
	et := at.Elem
	res, err := c.convertSliceHelper(et, int(l), p)
	if err != nil {
		return nil, nil, err
	}
	return res, tcEfaceSliceType, nil
}

// p points to an slice.
func (c *tcConverter) convertSlice(p unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	st := (*slicetype)(unsafe.Pointer(t))
	et := st.Elem
	v := (*slice)(p)
	l := v.len

	// Slice may be nil,
	// then we should make interface to be nil.
	if v.array == nil {
		return nil, nil, nil
	}
	if !c.deepCopy && t == tcEfaceSliceType {
		var mustDeepCopy = false
		var a unsafe.Pointer
		for i := 0; i < l; i++ {
			e := add(v.array, uintptr(i)*et.Size())
			var err error
			if !mustDeepCopy {
				var tmpEface eface
				tmpEface.data, tmpEface._type, err = c.convertAny(e, et)
				if err != nil {
					return nil, nil, err
				}
				if tmpEface.data != (*eface)(e).data || tmpEface._type != (*eface)(e)._type {
					mustDeepCopy = true
					a = mallocgc(uintptr(v.len)*et.Size(), et, true)
					copyMem := uintptr(i) * et.Size()
					if i > 0 {
						bulkBarrierPreWriteSrcOnly(uintptr(a), uintptr(v.array), copyMem, et)
						memmove(a, v.array, copyMem)
					}
					*(*eface)(add(a, copyMem)) = tmpEface
				}
			} else {
				ie := (*eface)(add(a, uintptr(i)*et.Size()))
				var err error
				ie.data, ie._type, err = c.convertAny(e, et)
				if err != nil {
					return nil, nil, err
				}
			}
		}
		if mustDeepCopy {
			s := new(slice)
			s.array = a
			s.len = v.len
			s.cap = v.len
			return unsafe.Pointer(s), tcEfaceSliceType, nil
		}
		return p, tcEfaceSliceType, nil
	}
	res, err := c.convertSliceHelper(et, l, v.array)
	if err != nil {
		return nil, nil, err
	}
	return res, tcEfaceSliceType, nil
}

func (c *tcConverter) convertSliceHelper(et *_type, l int, p unsafe.Pointer) (unsafe.Pointer, error) {
	a := makeslice(tcInterfaceType, l, l)
	for i := 0; i < l; i++ {
		e := add(p, uintptr(i)*et.Size())
		to, t, err := c.convertAny(e, et)
		if err != nil {
			return nil, err
		}
		ie := (*eface)(add(a, uintptr(i)*tcInterfaceType.Size()))
		ie._type = t
		ie.data = to
	}
	res := new(slice)
	res.array = a
	res.len = l
	res.cap = l
	return unsafe.Pointer(res), nil
}

func (c *tcConverter) convertInterface(p unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	var err error
	p, t, err = c.unpackInterface(p, t)
	if err != nil {
		return nil, nil, err
	}
	if t == nil {
		if p != nil {
			return nil, nil, tcBadStateError(0)
		}
		return nil, nil, nil
	}
	return c.convertAny(p, t)
}

type tcNumber interface {
	~int | ~uint | ~int8 | ~uint8 | ~int16 | ~uint16 | ~int32 | ~uint32 |
		~int64 | ~uint64 | ~float32 | ~float64 | uintptr
}

func tcConvertNumber[T tcNumber](c *tcConverter, from unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	kind := t.Kind()
	if c.numberKind == tcToOrig {
		return tcConverterNumberToOrig(c, from, t)
	}
	if abi.Int <= kind && kind <= abi.Uintptr && c.numberKind == tcToJsonNumber {
		return tcNumberToString[T](c, from, t), tcJsonNumberType, nil
	}
	i := *(*T)(from)
	f := float64(i)
	if abi.Float32 <= kind && kind <= abi.Float64 {
		abs := abs(f)
		format := byte('f')
		if abs != 0 {
			if float32(abs) < 1e-6 || float32(abs) >= 1e21 {
				format = 'e'
			}
		}
		str := tcEscapeFloatSci(f, format, int(t.Size())*8)
		if c.numberKind == tcToJsonNumber {
			return str, tcJsonNumberType, nil
		}
		if kind == abi.Float32 {
			// float64(i) use machine instructions to extend float point,
			// but it will change decimal precision.
			// To guarantee JSON-equal, we have to first convert float32 to decimal
			// and then convert back.
			var err error
			f, err = tcParseFloat(*(*string)(str), 64)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return tcEscapeFloat64(f), tcFloat64Type, nil
}

var tcBasicTypes [15]*_type

func tcConverterNumberToOrig(c *tcConverter, from unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	t = tcBasicTypes[int(t.Kind())]
	if !c.deepCopy {
		return from, t, nil
	}
	switch t.Size() {
	case 1:
		return tcConvT8(*(*uint8)(from)), t, nil
	case 2:
		return convT16(*(*uint16)(from)), t, nil
	case 4:
		return convT32(*(*uint32)(from)), t, nil
	case 8:
		return convT64(*(*uint64)(from)), t, nil
	}
	return nil, nil, nil
}

func tcConvT8(val uint8) unsafe.Pointer {
	return unsafe.Pointer(&staticuint64s[val])
}

var (
	tcTrueStr  string
	tcFalseStr string
)

func (c *tcConverter) boolToString(fp unsafe.Pointer) unsafe.Pointer {
	b := *(*bool)(fp)
	if b {
		return unsafe.Pointer(&tcTrueStr)
	}
	return unsafe.Pointer(&tcFalseStr)
}

func tcNumberToString[T tcNumber](c *tcConverter, fp unsafe.Pointer, t *_type) unsafe.Pointer {
	i := *(*T)(fp)
	kind := t.Kind()
	if abi.Int <= kind && kind <= abi.Int64 {
		return c.convertIntToString(int64(i))
	} else if abi.Uint <= kind && kind <= abi.Uintptr {
		return c.convertUintToString(uint64(i))
	}
	return nil
}

func (c *tcConverter) interfaceToString(fp unsafe.Pointer, ft *_type) (unsafe.Pointer, error) {
	var err error
	fp, ft, err = c.unpackInterface(fp, ft)
	if err != nil {
		return nil, err
	}
	if ft == nil {
		return nil, tcBadStateError(0)
	}
	return c.convertAnyToString(fp, ft)
}

func (c *tcConverter) ptrToString(fp unsafe.Pointer, ft *_type) (unsafe.Pointer, error) {
	pt := (*ptrtype)(unsafe.Pointer(ft))
	fp = tcDereference(fp)
	if fp == nil {
		return nil, tcBadStateError(0)
	}
	return c.convertAnyToString(fp, pt.Elem)
}

func (c *tcConverter) convertAnyToString(from unsafe.Pointer, ft *_type) (unsafe.Pointer, error) {
	switch ft.Kind() {
	case abi.String:
		return from, nil
	case abi.Bool:
		return c.boolToString(from), nil
	case abi.Int:
		return tcNumberToString[int](c, from, ft), nil
	case abi.Int8:
		return tcNumberToString[int8](c, from, ft), nil
	case abi.Int16:
		return tcNumberToString[int16](c, from, ft), nil
	case abi.Int32:
		return tcNumberToString[int32](c, from, ft), nil
	case abi.Int64:
		return tcNumberToString[int64](c, from, ft), nil
	case abi.Uint:
		return tcNumberToString[uint](c, from, ft), nil
	case abi.Uint8:
		return tcNumberToString[uint8](c, from, ft), nil
	case abi.Uint16:
		return tcNumberToString[uint16](c, from, ft), nil
	case abi.Uint32:
		return tcNumberToString[uint32](c, from, ft), nil
	case abi.Uint64:
		return tcNumberToString[uint64](c, from, ft), nil
	case abi.Uintptr:
		return tcNumberToString[uintptr](c, from, ft), nil
	case abi.Interface:
		return c.interfaceToString(from, ft)
	case abi.Pointer:
		return c.ptrToString(from, ft)
	default:
		return nil, tcUnsupportedConvertError(tcTypeName(ft) + " to string")
	}
}

//go:linkname tcConvertAnyToString runtime.tcConvertAnyToString
func tcConvertAnyToString(c *tcConverter, from unsafe.Pointer, ft *_type) (unsafe.Pointer, error) {
	return c.convertAnyToString(from, ft)
}

type tcArgumentError int
type tcArgumentTypeError int
type tcUnsupportedTypeError string
type tcBadStateError int
type tcUnsupportedConvertError string
type tcIllegalConvertError string
type tcCircularReference string

var tcNum []string

func (err tcArgumentError) Error() string {
	n := int(err)
	if n <= 1 {
		return "The " + tcNum[n] + " argument is nil"
	}
	return "Argument is nil"
}

func (err tcArgumentTypeError) Error() string {
	if int(err) == 0 {
		return "Argument is not struct type, map type or pointer to them"
	} else if int(err) == 1 {
		return "Argument is not slice type, array type or pointer to them"
	}
	return ""
}

func (err tcUnsupportedTypeError) Error() string {
	return "Unsupported type: " + string(err)
}

func (err tcBadStateError) Error() string {
	return "It occurs internal error: bad state"
}

func (err tcUnsupportedConvertError) Error() string {
	return "Unsupported Numabi.: " + string(err)
}

func (err tcIllegalConvertError) Error() string {
	return "Illegal conversion: " + string(err)
}

func (err tcCircularReference) Error() string {
	return "There may be circular references in the objects to be copied"
}

func tcIsEmptyValue(p unsafe.Pointer, t *_type) bool {
	kind := t.Kind()
	if kind <= abi.Float64 {
		return memequal(p, unsafe.Pointer(&zeroVal), t.Size())
	}
	switch kind {
	case abi.Array:
		return (*arraytype)(unsafe.Pointer(t)).Len == 0
	case abi.Map:
		return tcMaplen(p) == 0
	case abi.Slice:
		return (*slice)(p).len == 0
	case abi.String:
		return (*stringStruct)(p).len == 0
	case abi.Interface, abi.Pointer:
		return *(*unsafe.Pointer)(p) == nil
	}
	return false
}

func tcTypeOf(i interface{}) *_type {
	return (*eface)(unsafe.Pointer(&i))._type
}

const tcNSmalls = 100

var (
	tcDigits       unsafe.Pointer
	tcSmallsString unsafe.Pointer
)

func tcSmallNumber(i uint64) (unsafe.Pointer, int) {
	if i < 10 {
		return add(tcDigits, uintptr(i)), 1
	}
	return add(tcSmallsString, uintptr(i<<1)), 2
}

func (c *tcConverter) formatNumber(v uint64, neg bool) unsafe.Pointer {
	if !neg && v < tcNSmalls {
		str := c.ssBuffer.getElement(1)
		str.str, str.len = tcSmallNumber(v)
		return unsafe.Pointer(str)
	}

	u := v
	var j int
	if neg {
		j = 1
	}
	for {
		j++
		if u < 10 {
			break
		}
		u /= 10
	}

	var buf = c.bBuffer.getElement(j)
	str := c.ssBuffer.getElement(1)
	str.len = j
	str.str = unsafe.Pointer(buf)
	for v >= 100 {
		i := (v % 100) << 1
		v /= 100
		j -= 2
		memmove(add(unsafe.Pointer(buf), uintptr(j)), add(tcSmallsString, uintptr(i)), 2)
	}
	i := v << 1
	if v >= 10 {
		j -= 2
		memmove(add(unsafe.Pointer(buf), uintptr(j)), add(tcSmallsString, uintptr(i)), 2)
	} else {
		j--
		memmove(add(unsafe.Pointer(buf), uintptr(j)), add(tcSmallsString, uintptr(i+1)), 1)
	}
	if neg {
		*buf = '-'
	}
	return unsafe.Pointer(str)
}

func (c *tcConverter) convertIntToString(i int64) unsafe.Pointer {
	if i < 0 {
		return c.formatNumber(uint64(-i), true)
	}
	return c.formatNumber(uint64(i), false)
}

func (c *tcConverter) convertUintToString(i uint64) unsafe.Pointer {
	return c.formatNumber(i, false)
}

func tcEscapeFloat64(f float64) unsafe.Pointer {
	res := new(float64)
	*res = f
	return unsafe.Pointer(res)
}

//go:linkname tcGetBucketOffset
func tcGetBucketOffset(h unsafe.Pointer, mt *abi.MapType, key string, hash uintptr) (uintptr, uintptr, uintptr, uint8, bool)

//go:linkname tcGetTablePointer
func tcGetTablePointer(h unsafe.Pointer, idx uintptr) unsafe.Pointer

//go:linkname tcGetBucketPointer
func tcGetBucketPointer(h unsafe.Pointer, tt *abi.MapType, idx uintptr) unsafe.Pointer

//go:linkname tcGetSeed
func tcGetSeed(h unsafe.Pointer) uintptr

func tcMaplen(m unsafe.Pointer) int {
	return reflect_maplen((*maps.Map)(m))
}

//go:linkname tcHandleUsed
func tcHandleUsed(th, fh unsafe.Pointer)

//go:linkname tcHandleOverflow
func tcHandleOverflow(th unsafe.Pointer, table, bucket, index uintptr, tt *abi.MapType) (
	uintptr, unsafe.Pointer, unsafe.Pointer)

//go:linkname tcSetHash
func tcSetHash(tb unsafe.Pointer, index uintptr, hash uint8)

//go:linkname tcGetKeyOffset
func tcGetKeyOffset(index uintptr, mt *abi.MapType) uintptr

//go:linkname tcGetElemOffset
func tcGetElemOffset(index uintptr, mt *abi.MapType) uintptr

//go:linkname tcHandleEmpty
func tcHandleEmpty(tb unsafe.Pointer, index uintptr, th, tt unsafe.Pointer, mustDelete bool)

//go:linkname tcAllocMap
func tcAllocMap(fh unsafe.Pointer, t *abi.MapType) unsafe.Pointer

//go:linkname tcConvertMap
func tcConvertMap(c *tcConverter, p unsafe.Pointer, ft, tt *_type) (unsafe.Pointer, error)

// convertMap converts map into map[string]interface{}.
// If the input map is type of map[string]interface{} and we configure deepCopy,
// we can improve performance by skipping some elements that already meet the output requirements.
func (c *tcConverter) convertMap(p unsafe.Pointer, t *_type) (unsafe.Pointer, *_type, error) {
	data, err := tcConvertMap(c, p, t, tcStringEfaceMapType)
	if data == nil {
		return nil, nil, err
	}
	return data, tcStringEfaceMapType, err
}
