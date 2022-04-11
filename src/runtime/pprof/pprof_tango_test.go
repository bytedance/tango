// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pprof

import (
	"testing"
	"time"
)

func TestCPUProfileMultiCallstack(t *testing.T) {
	matches := matchAndAvoidStacks(stackContains, []string{"runtime/pprof.f3", "runtime/pprof.ffinal"}, avoidFunctions())
	testCPUProfile(t, matches, func(dur time.Duration) {
		f3()
	})
}

// Since f1, f2 are defined in the proto_test.go file, we start with f3
//
//go:noinline
func f3() {
	f4(120)
}

//go:noinline
func f4(depth int) {
	if depth == 0 {
		ffinal()
		return
	}
	f4(depth - 1)
}

//go:noinline
func ffinal() {
	var sum int
	for i := 0; i < 500000000; i++ {
		sum -= i / 2
		sum *= i
		sum /= i/3 + 1
		sum -= i / 4
	}
}
