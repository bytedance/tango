// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"runtime"
)

type S struct {
	I int
}

//go:noinline
func test() (r [10]*S, d [10]*S) {
	for i := 0; i < 10; i++ {
		r[i] = new(S)
		r[i].I = i
	}
	for i := 0; i < 10; i++ {
		d[i] = new(S)
		d[i].I = -i
	}
	runtime.GC()
	runtime.GC()
	runtime.GC()
	runtime.GC()
	runtime.GC()
	for i := 0; i < 10; i++ {
		d[i] = new(S)
		d[i].I = -i
	}
	for i := 0; i < 10; i++ {
		d[i] = new(S)
		d[i].I = -i
	}
	return
}

func main() {
	r, d := test()
	for i := 0; i < 10; i++ {
		if r[i].I != i {
			panic("oops")
		}
		if d[i].I != -i {
			panic("oops")
		}
	}
}
