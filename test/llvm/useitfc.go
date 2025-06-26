// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
)

//go:noinline
func p(s string) {
	fmt.Println("abc")
}

func main() {
	s := "haha"
	p(s)
}
