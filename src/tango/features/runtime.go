// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package features

import _ "unsafe"

//go:linkname setMaxHeapEnabled runtime.setMaxHeapEnabled
func setMaxHeapEnabled() bool

//go:linkname gcSetMaxHeap runtime.gcSetMaxHeap
func gcSetMaxHeap(bytes int64) int64

//go:linkname isBalanceGcEnabled runtime.isEvacEnabled
func isBalanceGcEnabled() bool

//go:linkname isGabEnabled runtime.isGabEnabled
func isGabEnabled() bool

//go:linkname isBeastModeEnabled runtime.isBeastModeEnabled
func isBeastModeEnabled() bool

//go:linkname getBeastModeInfo runtime.getBeastModeInfo
func getBeastModeInfo() string
