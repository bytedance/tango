// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

type ret struct {
	ret1 string
	ret2 int
	padd [10]int
}

func testPanic(a int) {
	defer func() {
		if err := recover(); err != nil {
			return
		}
		panic("not recovered")
	}()
	defer func() {
		fmt.Println("before recover")
	}()

	if a < 0 {
		callPanic("panic1")
		return
	}

	callPanic("panic2")
	return
}

//go:noinline
func callPanic(msg string) {
	panic(msg)
}

func main() {
	testPanic(1)
	testPanic(-1)
}
