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
		panic("we doesn't recover panic")
	}()
	var b int = 2
	fmt.Println(b)
	defer func() {
		fmt.Println("before recover")
		ret = 3
	}()

	defer func() {
		fmt.Println("before recover")
		ret = 3
	}()

	defer func() {
		fmt.Println("before recover")
		ret = 3
	}()

	defer func() {
		fmt.Println("before recover")
		ret = 3
	}()

	defer func() {
		fmt.Println("before recover")
		ret = 3
	}()

	defer func() {
		fmt.Println("before recover")
		ret = 3
	}()

	defer func() {
		fmt.Println("before recover")
		ret = 3
	}()

	defer func() {
		fmt.Println("before recover")
		ret = 3
	}()

	if a < 0 {
		callPanic("branch panic")
		return -1
	}

	callPanic("main panic")
	fmt.Println("second")
	return 4
}

//go:noinline
func callPanic(msg string) {
	panic(msg)
}

func main() {
	if a := testPanic(123); a != 123 {
		fmt.Println(a)
		panic("opendefer has failed")
	}
}
