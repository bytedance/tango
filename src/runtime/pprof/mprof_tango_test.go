// Copyright 2014 The Go Authors. All rights reserved.
// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Portions adapted from Go src/runtime/pprof/mprof_test.go.
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2025-09-16.

package pprof

import (
	"bytes"
	"internal/profile"
	"runtime"
	"testing"
)

//go:noinline
func allocateInGab() {
	for i := 0; i < 1024; i++ {
		memSink = new([128]byte)
	}
}

//go:noinline
func TestBalancedGCMemProfileStacks(t *testing.T) {
	// Disable sampling, otherwise it's difficult to assert anything.
	oldRate := runtime.MemProfileRate
	runtime.MemProfileRate = 1
	defer func() {
		runtime.MemProfileRate = oldRate
	}()

	// Allocate a meg to ensure that mcache.nextSample is updated to 1.
	for i := 0; i < 1024; i++ {
		memSink = make([]byte, 1024)
	}

	allocateInGab()
	memSink = nil

	runtime.GC()

	tests := []struct {
		expectedStk   []string
		skippedStk    []string
		unexpectedStk []string
	}{{
		expectedStk:   []string{"runtime/pprof.allocateInGab", "runtime/pprof.TestBalancedGCMemProfileStacks"},
		skippedStk:    []string{"runtime.systemstack_switch", "runtime.(*gab).refill", "runtime.mallocgc", "runtime.newobject"},
		unexpectedStk: []string{"runtime.mProfMallocGab", "runtime.profileAllocGab", "runtime.mallocgcSmallNoscan", "runtime.mallocgc1", "runtime.(*gab).refill.func1", "runtime.systemstack"},
	},
	}

	var buf bytes.Buffer
	if err := Lookup("heap").WriteTo(&buf, 0); err != nil {
		t.Fatalf("failed to write heap profile: %v", err)
	}
	p, err := profile.Parse(&buf)
	if err != nil {
		t.Fatalf("failed to parse heap profile: %v", err)
	}
	stks := profileStacks(p)
	for _, test := range tests {
		if !containsStack(stks, test.expectedStk) {
			t.Fatalf("No matching stack entry for %q\n\nProfile:\n%v\n stks: %v\n", test.expectedStk, p, stks)
		}

		if containsStack(stks, test.skippedStk) {
			t.Fatalf("Skipped stack entry matched for %q\n\nProfile:\n%v \nstks: %v\n", test.skippedStk, p, stks)
		}

		if containsStack(stks, test.unexpectedStk) {
			t.Fatalf("Unexpected stack entry matched for %q\n\nProfile:\n%v\n stks: %v\n", test.unexpectedStk, p, stks)
		}
	}
}
