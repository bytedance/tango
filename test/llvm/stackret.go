// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"unsafe"
)

type AllRegIfFirstStruct struct {
	a [9]int
	F [14]float64
}

type StackStruct struct {
	a [10]int
	F [10]float64
	f [10]float32
}

type StackIntStruct struct {
	A [10]int64
}

//go:noinline
func test2(s StackIntStruct) (r StackIntStruct) {
	for i := 0; i < 10; i++ {
		r.A[i] = s.A[i] & int64(0x00FFFFFFFFEFEFAA)
	}
	return
}

//go:noinline
func test3(s AllRegIfFirstStruct, s2 StackStruct) (r StackStruct) {
	touchAddress(unsafe.Pointer(&s2.a))
	for i := 0; i < 9; i++ {
		r.a[i] = s.a[i] + s2.a[i]
		r.F[i] = s.F[i] + s2.F[i]
	}
	return
}

//go:noinline
func test(s StackStruct) (r StackStruct) {
	for i := 0; i < 10; i++ {
		r.a[i] = s.a[i] + 3
	}
	for i := 0; i < 10; i++ {
		r.F[i] = s.F[i] + 3.0
	}
	for i := 0; i < 10; i++ {
		r.f[i] = s.f[i] - 5.0
	}
	for i := 0; i < 10; i++ {
		r.F[i] = s.F[i] + 4.0
	}
	for i := 0; i < 10; i++ {
		r.f[i] = s.f[i] - 6.0
	}
	return
}

//go:noinline
func touchAddress(p unsafe.Pointer) uintptr {
	if uintptr(p) == 123 {
		panic("Oops")
	}
	return uintptr(p)
}

func main() {
	var s StackStruct
	var s2 StackIntStruct
	for i := 0; i < 10; i++ {
		s.a[i] = i
		s.f[i] = float32(-i)
		s.F[i] = float64(i)

		s2.A[i] = int64(i)
	}
	expect1 := StackStruct{
		a: [...]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		F: [...]float64{4, 5, 6, 7, 8, 9, 10, 11, 12, 13},
		f: [...]float32{-6, -7, -8, -9, -10, -11, -12, -13, -14, -15},
	}
	expect2 := StackIntStruct{
		A: [...]int64{0, 0, 2, 2, 0, 0, 2, 2, 8, 8},
	}
	out1 := test(s)
	out2 := test2(s2)
	if expect1 != out1 {
		panic(fmt.Errorf("Oops, expect %v, got %v", expect1, out1))
	}
	if expect2 != out2 {
		panic(fmt.Errorf("Oops, expect %v, got %v", expect2, out2))
	}

	var s3 AllRegIfFirstStruct
	for i := 0; i < 9; i++ {
		s3.a[i] = i
		s3.F[i] = float64(i)
	}
	expect3 := StackStruct{
		a: [...]int{0, 2, 4, 6, 8, 10, 12, 14, 16, 0},
		F: [...]float64{0, 2, 4, 6, 8, 10, 12, 14, 16, 0},
	}
	out3 := test3(s3, s)
	if expect3 != out3 {
		panic(fmt.Errorf("Oops, expect %v, got %v", expect3, out3))
	}
}
