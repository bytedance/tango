// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

func testPanic(a int) (ret int) {
	var c int = 1
	fmt.Println(c)
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
			fmt.Println("we have recover panic")
			ret = a
			return
		}
	}()
	var b int = 2
	fmt.Println(b)
	defer func() {
		fmt.Println("before recover")
	}()

	if a < 0 {
		return -1
	}

	fmt.Println("second")
	return 4
}

//go:noinline
func callPanic(msg string) {
	panic(msg)
}

func main() {
	if a := testPanic(5); a != 4 {
		fmt.Println(a)
		panic("opendefer has failed")
	}
}
