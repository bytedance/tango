// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "unsafe"

type a struct {
	f1 int
	f2 float64
}

type b struct {
	f1 int
	f2 float64
}

func main() {
	v := a{
		1,
		2.0,
	}
	v2 := (*b)(unsafe.Pointer(&v))
	if v2.f1 != 1 || v2.f2 != 2.0 {
		panic("copy has fail")
	}
}
