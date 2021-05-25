// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstats

import (
	_ "unsafe"
)

//go:linkname gabMaxAllocSize runtime.gabGetMaxAllocSize
func gabMaxAllocSize() uint32

//go:linkname gabStatus runtime.gabStatus
func gabStatus() string

//go:linkname tangoNumSizes runtime.tangoNumSizes
func tangoNumSizes() int

//go:linkname tangoReadMemStats runtime.tangoReadMemStats
func tangoReadMemStats() ([]uint32, []uint64)
