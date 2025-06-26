// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type st struct {
	a int
	b float64
}

var global st

func mainHelper() *st {
	return &global
}

func main() {
	n1 := mainHelper()
	n2 := mainHelper()
	if n1 != n2 {
		panic("neqptr has failed")
	}
	if n1 == n2 {
		return
	}
	panic("eqptr has failed")
}
