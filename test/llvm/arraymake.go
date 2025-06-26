// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"math/rand"
)

func testArrayMake0() [0]int {
	a := [0]int{}
	return a
}

func testArrayMake1() bool {
	a := make([]int, 1)
	a[0] = rand.Int()
	return len(a) != 1
}

func testArraySelect() bool {
	a := [5]int{1, 2, 3, 4, 5}
	b := a[0] + 1
	fmt.Println(a)
	return b != 2
}

func main() {
	if len(testArrayMake0()) != 0 {
		panic("oparraymake0 has failed")
	}
	if testArrayMake1() {
		panic("oparraymake1 has failed")
	}
	if testArraySelect() {
		panic("oparrayselect has failed")
	}
}
