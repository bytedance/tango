// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dynconf

import (
	_ "unsafe"
)

//go:linkname listRuntimeConfigs runtime.rtCfgListAll
func listRuntimeConfigs() map[string]any

// gcSetMaxHeap is provided by package runtime.
//
//go:linkname gcSetMaxHeap runtime.gcSetMaxHeap
func gcSetMaxHeap(bytes int64) int64
