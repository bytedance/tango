// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

func main() {
	builderTest()
}

type BB struct {
	b *BB
	a int
}

//go:noinline
func builderTest() {
	var b BB

	b.a = 'a'
	fmt.Println(b)
}
