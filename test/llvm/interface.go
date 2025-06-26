// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"unsafe"
)

const size = 1 << 6

//go:noinline
func unsafePointerSlice() {
	var m [size]unsafe.Pointer
	for i := 0; i < size; i++ {
		e := i
		m[i] = unsafe.Pointer(&e)
	}

	for i := 0; i < size; i++ {
		item := m[i]
		e := *(*int)(item)
		if e != i {
			panic("value not correct")
		}
	}
}

func main() {
	unsafePointerSlice()
	testInterface1()
}

type shape interface {
	Area() int
}

type Circle struct {
	radius int
}

type rectangle struct {
	length int
	height int
}

//go:noinline
func (c Circle) Area() int {
	return 3 * c.radius * c.radius
}

func (r rectangle) Area() int {
	return r.length * r.height
}

//go:noinline
func testInterface1() {
	shapes := []shape{Circle{2}, rectangle{100, 200}}

	if shapes[0].Area() != 12 || shapes[1].Area() != 20000 {
		panic("interface call failed")
	}

	if testInterface2(shapes[0]) != 1 {
		panic("interface type is not correct")
	}
}

//go:noinline
func testInterface2(m interface{}) int {
	switch m.(type) {
	case Circle:
		return 1
	case rectangle:
		return 2
	default:
		panic("invalid interface type")
	}

}
