// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type St struct {
	I int
}

//go:noinline
func GetS() interface{} {
	var S = struct {
		I int
	}{I: 1}
	return S
}

//go:noinline
func main() {
	var S = struct {
		I int
	}{I: 1}
	if S != GetS() {
		panic("Oops")
	}
	s := St{I: 1}
	if GetS() == s {
		panic("Oops")
	}
}
