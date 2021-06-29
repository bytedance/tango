// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64
// +build amd64

package runtime_test

import (
	"internal/abi"
	"internal/goarch"
	"runtime"
	"testing"
)

// Note: should sync with cmd/compile/internal/ssagen/ssa.go.
const gGabTopGabOffset = 58 * goarch.PtrSize
const gGabEndGabOffset = 59 * goarch.PtrSize

func TestGabOffset(t *testing.T) {
	gabTop := runtime.GetGABTopOffset()
	gabEnd := runtime.GetGABEndOffset()
	if gabTop != abi.GabTopOffset {
		t.Fatalf("gabTop wrong offset %d, want %d", gabTop, abi.GabTopOffset)
	}
	if gabEnd != abi.GabEndOffset {
		t.Fatalf("gabEnd wrong offset %d, want %d", gabEnd, abi.GabEndOffset)
	}
}

const goidOffset = 152

func TestGoidOffset(t *testing.T) {
	goid := runtime.GetGoidOffset()
	if goid != goidOffset {
		t.Fatalf("goid wrong offset %d, want %d", goid, goidOffset)
	}
}
