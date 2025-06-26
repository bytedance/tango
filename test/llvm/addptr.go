// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"reflect"
	"unsafe"
)

func main() {
	a := [8]int{}
	for i := range a {
		a[i] = i
	}
	fmt.Println(reflect.TypeOf(a))
	b := *(*int)(unsafe.Pointer(uintptr(unsafe.Pointer(&a)) + unsafe.Sizeof(1)*3))
	if b != 3 {
		fmt.Println(b)
		panic("addPtr failed")
	}
}
