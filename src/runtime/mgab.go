// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/abi"
	"unsafe"
)

// Goroutine Allocation Buffer (a.k.a GAB)
// A fast bump-pointer style, per-goroutine allocation buffer.
//
// Based on the observation that generational hypothesis still works
// for certain Go workloads, such as micro-services.
//
// This approach intends to mitigate following problems:
//   - runtime.mallocgc is heavy when comparing to bump-pointer style allocation,
//     this patch tries to group many calls of runtime.mallocgc() into one.
//   - runtime.mallocgc allocated objects of different sizes usually far from
//     each other, which is not cache-friendly.
//     This patch tries to allocate recent small, noscan objects together.
type gab struct {
	// this pointer is to keep the in-use buffer memory alive across GC
	// even if no live objects reside in [base, top); so we can continue
	// to allocate in the remaining space [top, end) after GC.
	base     unsafe.Pointer
	top, end uintptr
}

const (
	gabCapShift      = 12
	gabCapInitial    = 1 << gabCapShift
	gabCapMaxDefault = 4 << gabCapShift
	gabMaxAllocSize  = abi.GabMaxAllocSize
)

var gabCapMax uintptr = gabCapMaxDefault // default value of gab Max Capacity.

func (buf *gab) clear() {
	buf.base = nil
	buf.top = 0
	buf.end = 0
}

func (buf *gab) refill() {
	ncap := ((buf.end - uintptr(buf.base)) << 1)
	if ncap == 0 {
		// uninitialized or cleared
		ncap = uintptr(gabCapInitial)
	} else if ncap > gabCapMax {
		// grown to max
		ncap = gabCapMax
	}

	// actually allocate memory
	buf.base = mallocgc1(ncap, nil, true)
	buf.top = uintptr(buf.base)
	buf.end = buf.top + ncap
}

func (buf *gab) remaining() uintptr {
	return buf.end - buf.top
}
