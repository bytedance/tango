// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"time"
)

//go:noinline
func test(t time.Time) int {
	w := t.Weekday()
	return int(w)
}

func main() {
	// Use a fixed UTC-7 offset so the timestamp is always a Monday.
	t := time.Unix(1745299851, 0).In(time.FixedZone("UTC-7", -7*60*60))
	w := test(t)
	if w != 1 {
		println(w)
		panic("oops")
	}
}
