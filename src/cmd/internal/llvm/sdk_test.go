// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package llvm

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestTangoLLVMSDKRoot(t *testing.T) {
	const customRoot = "/tmp/tangollvm-sdk"
	t.Setenv("TANGOLLVM_SDK_ROOT", customRoot)
	if got := tangoLLVMSDKRoot(); got != customRoot {
		t.Fatalf("tangoLLVMSDKRoot() = %q, want %q", got, customRoot)
	}

	t.Setenv("TANGOLLVM_SDK_ROOT", "")
	want := filepath.Join(runtime.GOROOT(), "pkg", "tangollvm")
	if got := tangoLLVMSDKRoot(); got != want {
		t.Fatalf("tangoLLVMSDKRoot() = %q, want %q", got, want)
	}
}
