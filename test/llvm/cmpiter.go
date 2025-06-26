// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type Builder interface {
	Test()
}

type S struct {
	I int
}

func (s *S) Test() {
	println("Test")
}

func test(a int) *S {
	if a < 0 {
		return nil
	}
	return &S{1}
}

func assertNil(b Builder) {
	if b != nil {
		panic("Oops")
	}
}

func assertNotNil(b Builder) {
	if b == nil {
		panic("Oops")
	}
}

func main() {
	var b Builder
	assertNil(b)
	b = test(-1)
	assertNotNil(b)
	b = test(1)
	assertNotNil(b)
}
