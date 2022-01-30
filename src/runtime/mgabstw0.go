// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || (!linux && !darwin)
// +build !amd64,!arm64 !linux,!darwin

package runtime

import (
	"internal/runtime/sys"
	"unsafe"
)

// A GAB implementation for GC.
// This data structure is outside heap
// and possess no pointer to heap when GC is inactive.
type stwgab struct {
	_              sys.NotInHeap
	cindex         int32 // chunk index
	base, top, end uintptr
	span           *mspan
}

func (ab *stwgab) clear() {
	shouldNotReachHere()
}

func (ab *stwgab) stwMalloc(size uintptr) unsafe.Pointer {
	shouldNotReachHere()
	return nil
}

func gabgcinit() {
	shouldNotReachHere()
}

func gabInitStwChunks(totalSize uintptr) {
	shouldNotReachHere()
}

func gabClearStwChunks() {
	shouldNotReachHere()
}

func (ab *stwgab) remaining() uintptr {
	shouldNotReachHere()
	return 0
}

func shouldNotReachHere() {
	throw("Should not reach here")
}
