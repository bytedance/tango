// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package abi

import "internal/goarch"

// Gab inline related constants.
const (
	GabTopOffset    = 59 * goarch.PtrSize // must match gp.ab.top in runtime/runtime2.go and runtime/mgab.go
	GabEndOffset    = 60 * goarch.PtrSize // must match gp.ab.end in runtime/runtime2.go and runtime/mgab.go
	GabMaxAllocSize = 128                 // Maximum allocation request size that GAB supports.
)
