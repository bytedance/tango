// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

// This variable ignores the atomic inherency of atomic.Int32
var ptrGCPercent *int32 = (*int32)(unsafe.Pointer(&gcController.gcPercent))
var ptrMemoryLimit *int64 = (*int64)(unsafe.Pointer(&gcController.memoryLimit))
var ptrGcThrptGoal *float64 = (*float64)(unsafe.Pointer(&gcAdaptiveController.latestGcThroughputGoal))

var pgoSource string

// setPGOSource is used to write pgo source to runtime.pgoSource because
// runtime/debug import runtime. So in runtime package, we can't use API
// in runtime/debug package to read buildInfo. We need to read pgoSource
// in another package and pass it to runtime.
//
//go:linkname setPGOSource
func setPGOSource(value string) {
	pgoSource = value
}

var rtCfgMap = map[string]any{
	"runtime.gab,ro":                 &debug.gab,
	"runtime.gabUsedBytes,ro":        &gabUsedBytes,
	"runtime.gabThreshold,ro":        &gabState.gabMemLimit,
	"runtime.gabMemoryPercent,rw":    &gabMemoryPercent,
	"runtime.gabEnabled,ro":          &gabEnabled,
	"runtime.gctrace,rw":             &debug.gctrace,
	"runtime.buildVersion,ro":        &buildVersion,
	"runtime.tangoVersion,ro":        &tangoVersion,
	"runtime.optMode,ro":             &optMode,
	"runtime.pgoSource,ro":           &pgoSource,
	"runtime.gcPercent,ro":           ptrGCPercent,
	"runtime.maxHeapBytes,ro":        &maxHeap,
	"runtime.largeAllocTrace,rw":     &debug.largealloctrace,
	"runtime.asyncpreemptoff,ro":     &debug.asyncpreemptoff,
	"runtime.madvdontneed,ro":        &debug.madvdontneed,
	"runtime.harddecommit,ro":        &debug.harddecommit,
	"runtime.memoryLimit,ro":         ptrMemoryLimit,
	"runtime.beastFlag,ro":           &beastFlag,
	"runtime.gcadaptivetrace,rw":     &debug.gcadaptivetrace,
	"runtime.gcadaptive,rw":          &debug.gcadaptive,
	"runtime.gcadaptiveThrptGoal,rw": ptrGcThrptGoal,
}

//go:linkname rtCfgListAll
func rtCfgListAll() map[string]any {
	return rtCfgMap
}
