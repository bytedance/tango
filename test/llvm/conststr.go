// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

const (
	S0  = ""
	S1  = "A"
	S8  = "ABCDEFGH"
	S16 = S8 + S8
	S32 = S16 + S16
	S64 = S32 + S32
)

//go:noinline
func test(i int) string {
	switch i {
	case 0:
		return S0
	case 1:
		return S1
	case 8:
		return S8
	case 16:
		return S16
	case 32:
		return S32
	case 64:
		return S64
	}
	panic("Oops")
}

func main() {
	if test(0) != S0 {
		panic("Oops")
	}
	if test(1) != S1 {
		panic("Oops")
	}
	if test(8) != S8 {
		panic("Oops")
	}
	if test(16) != S16 {
		panic("Oops")
	}
	if test(32) != S32 {
		panic("Oops")
	}
	if test(64) != S64 {
		panic("Oops")
	}
}
