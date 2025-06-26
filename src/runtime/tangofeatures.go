// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/bytealg"
	"internal/goexperiment"
	_ "unsafe"
)

//go:linkname isGabEnabled
func isGabEnabled() bool {
	return debug.gab > 0
}

//go:linkname isBeastModeEnabled
func isBeastModeEnabled() bool {
	// TODO: change it to optmode == "standard" when beast default enable.
	return optmode == "beast"
}

//go:linkname isLLVMEnabled
func isLLVMEnabled() bool {
	if !goexperiment.TangoLLVM {
		return false
	}
	switch GOARCH {
	case "amd64":
		return bytealg.IndexString(beastFlag, "llvm=") >= 0 &&
			bytealg.IndexString(beastFlag, "llvm=0") < 0
	default:
		return false
	}
}

//go:linkname isAdaptSchedEnabled
func isAdaptSchedEnabled() bool {
	return debug.adapt > 0
}

//go:linkname getBeastModeInfo
func getBeastModeInfo() string {
	return optmode
}

//go:linkname getAdaptSchedOptsInfo
func getAdaptSchedOptsInfo() string {
	return artsOpts
}

//go:linkname isModuleDataOptEnabled
func isModuleDataOptEnabled() bool {
	return debug.moduledataopt > 0
}
