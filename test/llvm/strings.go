// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"html"
	"strings"
)

func main() {
	indexTest()
	builderTest()
	escape()
}

//go:noinline
func indexTest() {
	n := strings.Index("", "")

	if n != 0 {
		panic("Index failed")
	}

	n = strings.Index("abc", "b")
	if n != 1 {
		panic("index failed")
	}

	n = strings.Index("ababababac", "ac")
	if n != 8 {
		panic("index failed")
	}

}

//go:noinline
func builderTest() {
	var b strings.Builder

	n, _ := b.WriteString("Haha")
	if b.String() != "Haha" || n != 4 {
		panic("String is not as expected")
	}

	n, _ = b.WriteString("world")
	if b.String() != "Haha world" && n != 5 {
		panic("writestring fail")
	}
}

//go:noinline
func escape() {
	s := "hhh&xxx'hhh<xxx"
	res := html.EscapeString(s)

	if res != "hhh&amp;xxx&#39;hhh&lt;xxx" {
		panic("html escape failed")
	}
}
