// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"reflect"
)

// reflect.funcLayout has lots of duffcopy

//go:noinline
func foo(p *int, b []byte) {

}

//go:noinline
func copyBlock(dst *[512]byte, src [512]byte) { *dst = src }

//go:noinline
func zeroBlock(dst *[512]byte) { *dst = [512]byte{} }

func main() {
	var src [512]byte
	for i := range src {
		src[i] = byte(i + 1)
	}
	dst := struct {
		before byte
		data   [512]byte
		after  byte
	}{before: 17, after: 29}
	copyBlock(&dst.data, src)
	if dst.data != src || dst.before != 17 || dst.after != 29 {
		panic("Duff copy changed data or surrounding bytes")
	}
	zeroBlock(&dst.data)
	if dst.data != [512]byte{} || dst.before != 17 || dst.after != 29 {
		panic("Duff zero changed data or surrounding bytes")
	}

	fn := reflect.ValueOf(foo)
	var i int
	var b []byte
	p := &i
	fn.Call([]reflect.Value{
		reflect.ValueOf(p),
		reflect.ValueOf(b)})
}
