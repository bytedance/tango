// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

func main() {
	a := complex(3.2, 1.2)
	b := complex(4.2, 4.2)
	if a-b != complex(-1.0, -3.0) {
		panic("complex has fail")
	}
}
