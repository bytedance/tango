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
	var n1 interface{} = mainHelper()
	var n2 interface{} = mainHelper()
	if n1 != n2 {
		panic("idata has failed")
	}
}
