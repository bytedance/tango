// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package llvm

import (
	"os"
	"path/filepath"
	"runtime"
)

func tangoLLVMSDKRoot() string {
	if root := os.Getenv("TANGOLLVM_SDK_ROOT"); root != "" {
		return root
	}
	return filepath.Join(runtime.GOROOT(), "pkg", "tangollvm")
}
