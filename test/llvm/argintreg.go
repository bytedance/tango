// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

type ret struct {
	ret1 string
	ret2 int
}

func testArgIntReg(retVal ret) bool {
	if retVal.ret2 != 1 {
		fmt.Println(retVal)
		return true
	}
	return false
}

func main() {
	a := ret{
		"main",
		1,
	}
	if testArgIntReg(a) {
		panic("opargintreg has failed")
	}
}
