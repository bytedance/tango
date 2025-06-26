// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"unsafe"
)

func Float64bits(f unsafe.Pointer) uintptr {
	return uintptr(f)
}

func main() {
	if Float64bits(unsafe.Pointer(nil)) != 0 {
		panic("convert has failed")
	}
}
