// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conv

import (
	"encoding"
	"encoding/json"
	"math/big"
	"reflect"
	"strconv"
	"sync"
	"unsafe"
)

var convertPool = sync.Pool{New: newConverter}

func getConverter() any {
	return convertPool.Get()
}

func putConverter(c any) {
	convertPool.Put(c)
}

func marshalText(i encoding.TextMarshaler) ([]byte, error) {
	if b, err := i.MarshalText(); err != nil {
		return nil, err
	} else {
		return stringBytes(b), nil
	}
}

func marshalJson(i json.Marshaler) ([]byte, error) {
	return i.MarshalJSON()
}

func init() {
	initTypes(reflect.TypeOf(json.Number("")), &big.Int{}, &big.Int{})
	initValues()
	initFuncs(escapeFloatSci, getConverter, putConverter, strconv.ParseFloat,
		extractStructFields, parseInterface, marshalJson, marshalText)
}

func escapeFloatSci(f float64, format byte, bitSize int) unsafe.Pointer {
	b := strconv.AppendFloat(make([]byte, 0, 24), f, format, -1, bitSize)
	if format == 'e' {
		// clean up e-09 to e-9
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return unsafe.Pointer(&b)
}

//go:linkname toStringMap runtime.tcToStringMap
func toStringMap(from interface{}, preferredNumberKind reflect.Type, deepCopy bool) (map[string]interface{}, error)

//go:linkname typeToType runtime.tcTypeToType
func typeToType(from, to interface{}, deepCopy bool) error

//go:linkname initValues runtime.tcInitValues
func initValues()

//go:linkname initTypes runtime.tcInitTypes
func initTypes(jsonNumberReflectType reflect.Type, bigIntTextMarshal encoding.TextMarshaler, bigIntJsonMarshaler json.Marshaler)

//go:linkname initFuncs runtime.tcInitFuncs
func initFuncs(escapeFloatSci func(f float64, format byte, bitSize int) unsafe.Pointer, getConverter func() any,
	putConverter func(any), parseFloat func(s string, bitSize int) (float64, error),
	extractStructFields func(t reflect.Type) ([]bool, [][]int, []string, []bool),
	parseInterface func(b []byte, useNumber bool) any,
	marshalJson func(i json.Marshaler) ([]byte, error),
	marshalText func(i encoding.TextMarshaler) ([]byte, error))

//go:linkname newConverter runtime.tcNewConverter
func newConverter() any
