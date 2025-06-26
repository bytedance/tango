// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

//go:noinline
func divide(a, b int64) int64 {
	return a / b
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			return
		}
		panic("no divide-by-zero error!")
	}()
	divide(100, 0)
}
