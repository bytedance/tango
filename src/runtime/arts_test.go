// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package runtime_test

import (
	"internal/race"
	"runtime"
	"strings"
	"testing"
)

func TestAdaptRTSched(t *testing.T) {
	if race.Enabled {
		t.Skip("skipping test in race mode")
	}
	output := runTestProg(t, "testadapt", "adapt", "GODEBUG=adapt=1,adapttrace=1")
	want := []string{
		"All goroutines finished as expected",
	}
	for _, w := range want {
		if !strings.Contains(output, w) {
			t.Fatalf("output:\n%s\n\nwanted:\n%s", output, w)
		}
	}
}

func testArtOpt(t *testing.T, opt int32, nv int32, expected bool) {
	if runtime.ArtsOptEnabled(opt) != expected {
		t.Fatalf("artsOptEnabled failed, opt: 0x%x, debug.adapt: 0x%x, got: %t, expected: %t",
			opt, nv, !expected, expected)
	}
}

// Sanity checking to test the basics of option handling.
func TestArtOpt(t *testing.T) {
	nv := int32(1)
	defer runtime.SetArtsOpt(runtime.SetArtsOpt(nv))

	testArtOpt(t, 1, nv, true)
	testArtOpt(t, 2, nv, true)
	testArtOpt(t, 8, nv, true)

	nv = int32(2 | 4)
	runtime.SetArtsOpt(nv)
	testArtOpt(t, 1, nv, false)
	testArtOpt(t, 2|4, nv, true)
	testArtOpt(t, 2, nv, true)
	testArtOpt(t, 4, nv, true)
	testArtOpt(t, 8, nv, false)
}
