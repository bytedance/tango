// Copyright 2026 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"math"
	"runtime"
	"testing"
)

func sigmoidMathExp(x float64, k float64, target float64) float64 {
	return 1.0 / (1.0 + math.Exp(-k*(x-target)))
}

func TestSigmoidApprox(t *testing.T) {
	k := 3.0
	target := 0.0
	start := -100.0
	end := 100.0
	step := 0.1
	num := int((end-start)/step) + 1
	testPoints := make([]float64, num)
	for i := 0; i < num; i++ {
		testPoints[i] = start + float64(i)*step
	}
	tolerance := 1e-3

	for _, x := range testPoints {
		expected := sigmoidMathExp(x, k, target)
		actual := runtime.SigmoidApprox(x, k, target)
		if math.Abs(expected-actual) > tolerance {
			t.Errorf("sigmoidApprox(%v, %v, %v) = %v; want %v", x, k, target, actual, expected)
		}
	}
}
