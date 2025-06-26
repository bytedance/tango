// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "fmt"

//go:noinline
func test64(a, u, c int64, m *int64) (int64, int64, int64, int64) {
	return u / 5, a, c, *m / 5
}

func main() {
	tests := [...]int64{
		0, 0,
		1, 0,
		5, 1,
		6, 1,
		// ...
	}

	var ia, ic int64
	ia, ic = 100, 200 // some large number not in array tests
	for i := 0; i < len(tests); i += 2 {
		in := tests[i+0]
		out := tests[i+1]
		if run, a, c, m := test64(ia, in, ic, &in); run != out || a != ia || c != ic || m != out {
			panic(fmt.Errorf("expected (%d,%d,%d,%d), got (%d,%d,%d,%d)", out, ia, ic, out, run, a, c, m))
		}
	}
}
