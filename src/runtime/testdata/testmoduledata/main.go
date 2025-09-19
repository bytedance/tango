// Copyright 2015 The Go Authors. All rights reserved.
// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Portions adapted from Go src/runtime/testdata/testprog/main.go.
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2025-09-19.

// Package main to move policy tests into separated process
// to mitigate the impact of other concurrently running testcases.
package main

import (
	"os"
)

var (
	cmds = map[string]func(){}
)

func register(name string, f func()) {
	if cmds[name] != nil {
		panic("duplicate registration: " + name)
	}
	cmds[name] = f
}

func registerInit(name string, f func()) {
	if len(os.Args) >= 2 && os.Args[1] == name {
		f()
	}
}

func main() {
	if len(os.Args) < 2 {
		println("usage: " + os.Args[0] + " name-of-test")
		return
	}
	f := cmds[os.Args[1]]
	if f == nil {
		println("unknown function: " + os.Args[1])
		return
	}
	f()
}
