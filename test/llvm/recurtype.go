// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type T1 ***T1
type T2 *[2]T2
type St struct {
	t T3
}
type T3 *St

type T4 *T1
type T5 *T2

//go:noinline
func test(t1 T1, t2 T2, t3 T3, t4 T4, t5 T5) {
}

func main() {
	test(nil, nil, nil, nil, nil)
}
