// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

type ret struct {
	ret1 string
	ret2 int
}

func testPanic(a int) (ret, bool) {
	var retVal ret
	var c int = 1
	fmt.Println(c)
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
			fmt.Println("we have recover panic")
			retVal.ret2 = a
			retVal.ret1 = "recover"
			fmt.Println(retVal)
			return
		}
		retVal.ret1 = "not recover"
		panic(retVal.ret1)
	}()
	var b int = 2
	fmt.Println(b)
	defer func() {
		fmt.Println("before recover")
	}()

	if a < 0 {
		retVal.ret1 = "not recover"
		callPanic(retVal.ret1)
		return retVal, true
	}

	retVal.ret1 = "main panic"
	callPanic(retVal.ret1)
	fmt.Println("second")
	return retVal, true
}

//go:noinline
func callPanic(msg string) {
	panic(msg)
}

func main() {
	if a, ok := testPanic(5); a.ret1 != "" || a.ret2 != 0 || ok {
		fmt.Println(a)
		fmt.Println(ok)
		panic("opendefer has failed")
	}
}
