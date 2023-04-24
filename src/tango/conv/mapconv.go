// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conv

import (
	"reflect"
)

// Converter is used to control the behavior of data conversion.
// NumKind specifies the conversion type of number.
// Option value of NumKind are reflect.Type(interface{}{}), reflect.Type(float64(0)), or reflect.Type(json.Number("")).
// Default value of NumKind is reflect.Type(interface{}{}) which means we convert number to itself.
// When NumKind is set to reflect.Type(float64(0)), we convert number to float64.
// when NumKind is set to reflect.Type(json.Number("")), we convert number to json.Number.
// DeepCopy specifies whether copy the data thought it already satisfies the requirements of the output format.
// Default value of Deepcopy is false which means we skip some conversion of elements when doing []interface{} -> []interface{},
// map[string]interface{} -> map[string]interface{}, bool -> bool, floatX -> floatX, intX -> intX, uintX -> uintX, string -> string,
// and we will get less cpu and memory usage.
type Converter struct {
	NumKind  reflect.Type
	DeepCopy bool
}

// ToStringMap does a data conversion. It converts argument to be map[string]interface{}.
// API specification:
// 1.Arguments should be struct types, map types, or pointers to them.
// 2.Fields or elements should not be complex64, complex128, chan, func, or unsafePointer.
// e.g.
//
//	type A struct {
//	  F1 complex64
//	  F2 complex128
//	  F3 chan
//	  F4 func
//	  F5 unsafe.Pointer
//	}
//
// Arguments shouldn't be types of struct A.
//
//	type B struct {
//	  F1  bool
//	  F2  int
//	  F3  int8
//	  F4  int16
//	  F5  int32
//	  F6  int64
//	  F7  uint
//	  F8  uint16
//	  F9  uint32
//	  F10 uint64
//	  F11 uintptr
//	  F12 float32
//	  F13 float64
//	  F14 [5]int
//	  F15 []int
//	  F16 interface{}
//	  F17 map[int]string
//	  F18 *int
//	  F19 []int
//	  F20 string
//	  F21 C
//	}
//
// type struct C { F1 string }
// Arguments can be types of struct B.
// 3.We convert int8, uint8, int16, uint16, int32, uint32, int64, uint64,
// int, uint, uintptr, float32, and float64 to the original type, which means
// int8 -> int8, int16 -> int16... by default, not all to float64 as encoding/json.
// You can configure it by assigning KindOrig, KindFloat64, or KindJsonNumber to NumKind.
// 4.The argument is guaranteed by the caller to have no circular references.
// e.g., the following should not happen:
// type A struct { B1 *B }
// type B struct { A1 *A }
// Arguments shouldn't be types of struct A or struct B.
// 5.The key type of map can only be bool, int, int8, int16, int32, int64, uint, uint8, uint16,
// uint32, uint64, uintptr, srting, interface to them, or pointer to them.
func (c *Converter) ToStringMap(from interface{}) (map[string]interface{}, error) {
	return toStringMap(from, c.NumKind, c.DeepCopy)
}

// TypeToType does a deep copy or converts one type to another.
// API specification:
// 1.TypeToType doesn't copy un-exported field or field tagged as json:"-" by default,
// but you can set Converter.DeepCopy true to copy all field.
// 2.Instead of reporting an error, TypeToType ignores it if we do an incorrect type conversion.
// e.g., type of "from" is map[string]interface{} and type of "to" is int.
// TypeToType does nothing and no error return.
// 3.The current legal conversions are as follows:
// structX1 -> structX2
// map[X1]['X1] -> map[X2]['X2]
// slice[X1] -> slice[X2]
// array[X1] -> array[X2]
// interfaceX1 -> interfaceX2
// pointerX1 -> pointerX2
// X1 -> X2, X1 can be pointer type whose element type is X2, interface type whose dynamic type is X2, or same as X2.
// X2 can be intX, uintX, floatX, structX, sliceX, arrayX, mapX, or interfaceX.
// 4.Bidirectional conversion of numeric types(such as int32 <-> int64, float32 <-> float64) will occur promotion or truncation.
// The result is equivalent to the following code in golang code:
// var from Y
// var to X = X(from)
// X, Y can be any of uint8, uint16, uint32, uint64, int, int8, int16, int32, int64, uint, uintptr, float32, float64.
// 5.The second argument should be pointer type.
func (c *Converter) TypeToType(from interface{}, to interface{}) error {
	return typeToType(from, to, c.DeepCopy)
}
