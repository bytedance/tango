// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math"
)

type ret struct {
	ret1 string
	ret2 float64
}

func testArgFloatReg(retVal ret) bool {
	if math.Abs(retVal.ret2-1.0) > 0.001 {
		fmt.Println(retVal)
		return true
	}
	return false
}

func main() {
	a := ret{
		"main",
		1.0,
	}
	if testArgFloatReg(a) {
		panic("opargfloatreg has failed")
	}
}
