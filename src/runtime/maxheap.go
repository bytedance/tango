// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/runtime/math"
	"internal/strconv"
	_ "unsafe"
)

// To begin with, maxHeap is infinity.
var maxHeap int64 = math.MaxInt64

// Indirectly export gcSetMaxHeap so that user can try to access it by declare a linkname,
// even when using an sdk that doesn't provide this variable, it will get a nil value instead of a compile error.
//
//go:linkname gcSetMaxHeapFunc
var gcSetMaxHeapFunc = gcSetMaxHeap

//go:linkname setMaxHeapEnabled
func setMaxHeapEnabled() bool {
	return maxHeap != math.MaxInt64
}

const (
	// GODEBUG=gcHeapPercent's maximum value, if it is greater than this value,
	// it will be limited to the maxHeapPercent.
	maxHeapPercent = 80
	// Maximum percent of gc_trigger compared to gc_goal, reserve enough margin for gc mark.
	maxTriggerPercent = 95
	// The minimum value of memoryLimitEnv, if it is less than this value, the maxHeap will not be enabled.
	minLimitMaxHeap = 100 * 1024 * 1024 // 100MB
	memoryLimitEnv  = "MY_MEM_LIMIT"    // instance available memory.
)

func gcPolicyInit() {
	if debug.gcHeapPercent > 0 {
		if debug.gcHeapPercent > maxHeapPercent {
			debug.gcHeapPercent = maxHeapPercent
		}
		if limitValue, err := strconv.Atoi(gogetenv(memoryLimitEnv)); err == nil {
			if limitValue < minLimitMaxHeap {
				return
			}
			gcSetMaxHeap(int64(limitValue * int(debug.gcHeapPercent) / 100))
		}
	}
}

//go:linkname gcSetMaxHeap
func gcSetMaxHeap(in int64) int64 {
	var out int64
	systemstack(func() {
		lock(&mheap_.lock)
		out = gcController.setMemoryLimit(in)
		if in >= 0 {
			maxHeap = in
		}
		if in < 0 || out == in {
			// If we're just checking the value or not changing
			// it, there's no point in doing the rest.
			unlock(&mheap_.lock)
			return
		}
		gcControllerCommit()
		unlock(&mheap_.lock)
	})
	return out
}
