// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

//go:noinline
func foo(a, b, c, d, e, f, g, h, i, j, k, l int, m float32) int {
	n := int(m)
	return a + b + c + d + e + f + g + h + i + j + k + l + n
}

type pp struct {
	a float32
	b int32
	d string
	e []byte
}

//go:noinline
func foo2(p pp) pp {
	p.b = p.b + 1
	return p
}

func main() {

	res := foo(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13.6)

	if res != 91 {
		panic("stack parameter tests fails")
	}

	p := pp{0.1, 2, "hello", []byte("world")}

	res2 := foo2(p)

	fmt.Println(res2)
}
