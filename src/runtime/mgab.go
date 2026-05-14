// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/abi"
	"internal/bytealg"
	"internal/goarch"
	"internal/runtime/atomic"
	"internal/runtime/gc"
	"internal/strconv"
	"unsafe"
)

// Goroutine Allocation Buffer (a.k.a GAB)
// A fast bump-pointer style, per-goroutine allocation buffer.
//
// Based on the observation that generational hypothesis still works
// for certain Go workloads, such as micro-services.
//
// This approach intends to mitigate following problems:
// - runtime.mallocgc is heavy when comparing to bump-pointer style allocation,
//   this patch tries to group many calls of runtime.mallocgc() into one.
// - runtime.mallocgc allocated objects of different sizes usually far from
//   each other, which is not cache-friendly.
//   This patch tries to allocate recent small, noscan objects together.
//

const (
	gabCapShift      = 10
	gabCapInitial    = 1 << gabCapShift
	gabCapMaxDefault = 1 << (gabCapShift + 4)
	// Maximum allocation request size that GAB supports.
	gabMaxAllocSize                    = abi.GabMaxAllocSize
	gabUsePolicyThreshold      uintptr = 1 << 25 // threshold that automatic policy starts to take effect, 32MB
	gabDefaultMemoryPercent            = 20
	gabDefaultPodMemoryPercent         = 10
	// Two size classes of objStartBits. See comments before newObjStartBits().
	smallObjStartBitsAllocSize uintptr = gabCapMaxDefault / goarch.PtrSize / 8
	largeObjStartBitsAllocSize uintptr = gc.MaxSmallSize / goarch.PtrSize / 8
	// The minimal size of map bucket that can have frozenBits.
	// See comments before newFrozenBits() for details.
	// This size is guarded by the test case TestGabMinFrozenMapBucketSize().
	minFrozenMapBucketSize uintptr = 136
	gabStrHeaderObjSize            = 16
)

var (
	gabCapMax             uintptr = gabCapMaxDefault // default value of gab Max Capacity.
	gabScanCapMax         uintptr = gabCapMax << 2   // maximum capacity for scan GABs.
	gabUsedBytes          uintptr                    // total consumed bytes
	gabStrHeaderUsedBytes uintptr                    // total consumed bytes of string header buffers
	strBufferAllocStat    uintptr                    // total allocated bytes of str buffer
	gabBufferAllocStat    uintptr                    // total allocated bytes of gab buffer
	// 'gabEnabled' control the runtime state of GAB feature,
	// mallocgc() will check this flag to determine if should goto fastpath.
	// on the other hand, 'runtime.debug.gab' represents user's intension
	// about whether GAB is expected to be enabled over all or not
	gabEnabled                 = false
	gabInliningEnabled         = false
	gabMemoryPercent   int32   = gabDefaultMemoryPercent // hard limit on total bytes used by GAB, 10% of memory target
	gabPolicyEnabled           = true                    // control whether policy should take effect
	safeMargin         uintptr = 120                     // Allocate more survivor GABs
)

var gabdebug struct {
	// Level of tracing for goroutine-allocation-buffer
	// 0: tracing disabled
	// 1: trace gab memory chunks
	// 2: trace mspan
	// 3: trace object, references
	trace int32
	// GAB evacuation frequency
	// 0 by default. No forced evacuation.
	// x: do evacuation every x GC cycle, which incurs overhead.
	evacuation int32
	// Whether enable GAB pacer strategy
	// 0 by default.
	// 1: enabled.
	// For testing only. Will be removed later.
	gabPacer int32
	// Whether enable GAB str allocation path.
	gabStrAlloc int32
	// Whether enable GAB str header allocation path.
	gabStrHeaderAlloc int32
	// Whether enable GAB debug allocation statistics.
	gabDebugStat int32
}

var gabvars = []dbgVar{
	{name: "gabPercent", value: &gabMemoryPercent},
	{name: "trace", value: &gabdebug.trace},
	{name: "evacuation", value: &gabdebug.evacuation},
	{name: "gabPacer", value: &gabdebug.gabPacer},
	{name: "gabStrAlloc", value: &gabdebug.gabStrAlloc},
	{name: "gabStrHeaderAlloc", value: &gabdebug.gabStrHeaderAlloc},
	{name: "gabDebugStat", value: &gabdebug.gabDebugStat},
}

func gabinit() {
	// parse gab environ option
	for p := gogetenv("GOGABOPT"); p != ""; {
		field := ""
		i := bytealg.IndexByteString(p, ',')
		if i < 0 {
			field, p = p, ""
		} else {
			field, p = p[:i], p[i+1:]
		}
		i = bytealg.IndexByteString(field, '=')
		if i < 0 {
			continue
		}
		key, value := field[:i], field[i+1:]

		for _, v := range gabvars {
			if v.name == key {
				if n, err := strconv.Atoi(value); err == nil {
					*v.value = int32(n)
				}
			}
		}
	}

	gabConfigInit()
}

// this initilization routine will be invoked MORE THAN ONCE
func gabConfigInit() {
	if debug.gab <= 0 {
		debug.gab = 0
		gabCapMax = 0
		gabEnabled = false
		return
	}

	if isEvacEnabled() && !((GOARCH == "amd64" || GOARCH == "arm64") && (GOOS == "linux" || GOOS == "darwin")) {
		println("Only on linux/{amd64,arm64} and darwin/{amd64,arm64} is balanced GC supported. Downgrade to gab=1.")
		debug.gab = 1
	}

	if gabMemoryPercent >= 100 || gabMemoryPercent < 0 {
		throw("Illegal gabMemoryPercent, should < 100 && >= 0")
	}

	if debug.gab <= 2 || debug.gab > maxSmallSize || debug.gab < gabCapInitial {
		gabCapMax = gabCapMaxDefault
		gabPolicyEnabled = true
	} else {
		gabCapMax = uintptr(debug.gab >> gabCapShift << gabCapShift)
		gabPolicyEnabled = false
	}
	// default values
	gabEnabled = true
	gabInliningEnabled = gabEnabled && !isEvacEnabled()

	gabState.init()
	if isEvacEnabled() {
		gabgcinit()
	}
}

// for testing only
//
//go:linkname gabGetCap
func gabGetCap() (capInit, capMax uintptr) {
	return gabCapInitial, gabCapMax
}

type gab struct {
	// This pointer is to keep the in-use buffer memory alive across GC
	// even if no live objects reside in [base, top); so we can continue
	// to allocate in the remaining space [top, end) after GC.
	base     unsafe.Pointer
	top, end uintptr // offset known to cmd/compile/internal/gc/ssa.go
	span     *mspan  // the mspan of current GAB, updated during GAB refilling
}

func (buf *gab) clear() {
	buf.base = nil
	buf.top = 0
	buf.end = 0
	buf.span = nil
}

func (buf *gab) refill() {
	buf.refillWithOptions(true, nil)
}

func (buf *gab) refillWithOptions(needzero bool, typ *_type) {
	ncap := buf.nextCapacity()
	noscan := typ == nil || !typ.Pointers()
	if !noscan && ncap > gabScanCapMax {
		ncap = gabScanCapMax
	}
	if isEvacEnabled() && noscan {
		if buf.remaining() > 0 {
			buf.fillRemaining()
		}
		// Prevent allocating GAB memory from being preempted by the GC.
		// Balanced GC requires pinning active GABs during the first stop the world.
		// If a GAB is allocated before that and is not attached to a g,
		// then it is missing to be pinned.
		systemstack(func() {
			base := mallocgc1(ncap, typ, needzero, true)
			buf.span = spanOf(uintptr(base))
			buf.base = base
		})
	} else {
		buf.base = mallocgc1(ncap, typ, needzero, true)
	}
	buf.top = uintptr(buf.base)
	buf.end = buf.top + ncap

	if gabdebug.trace >= 3 {
		s := spanOf(buf.top)
		if s == nil || !s.isGab() {
			print("gab.refill(): span=", s, " is not for GAB allocation\n")
			throw("Invalid span")
		}
		print("Alloc gab:g=", hex(uintptr(unsafe.Pointer(getg()))), ", [", hex(buf.top), "~", hex(buf.end), ")\n")
	}
}

// Fill GAB with filler object.
func (buf *gab) fillRemaining() {
	if !isEvacEnabled() {
		throw("Only for balanced GC.")
	}
	startObj(buf.top, buf.span)
}

func (buf *gab) nextCapacity() (size uintptr) {
	size = ((buf.end - uintptr(buf.base)) << 1)
	if size == 0 {
		// uninitialized or cleared
		size = uintptr(gabCapInitial)
	} else if size > gabCapMax {
		// grown to max
		size = gabCapMax
	}
	if size > gc.MaxSmallSize || size < gabCapInitial {
		throw("Illegal capacity")
	}
	// Round new capacity to precise small object slot size to reduce wasted space
	if gabCapMax != gabCapMaxDefault {
		// please update this code if corresponding code from runtime.mallocgc1 has changed
		var sizeclass uint8
		if size <= gc.SmallSizeMax-8 {
			sizeclass = gc.SizeToSizeClass8[divRoundUp(size, gc.SmallSizeDiv)]
		} else {
			sizeclass = gc.SizeToSizeClass128[divRoundUp(size-gc.SmallSizeMax, gc.LargeSizeDiv)]
		}
		size = uintptr(gc.SizeClassToSize[sizeclass])
	}
	return
}

func (buf *gab) remaining() uintptr {
	return buf.end - buf.top
}

// Mark the top of a GAB as a beginning of an object.
// This is only a mark and is used to get the size of
// the last object in the GAB.
//
//go:nowritebarrierrec
func (buf *gab) makeParsable() {
	if buf.remaining() > 0 {
		buf.fillRemaining()
	}
}

func (buf *gab) retire() {
	if isEvacEnabled() {
		buf.makeParsable()
	}
	buf.clear()
}

//go:linkname gabGetMaxAllocSize
func gabGetMaxAllocSize() uint32 {
	return gabMaxAllocSize
}

//go:linkname gabStatus
func gabStatus() string {
	var st string
	if debug.gab != 0 {
		st = "on"
		if !gabEnabled {
			st += "(paused)"
		}
	} else {
		st = "off"
	}
	return st
}

func isGabStrAllocEnabled() bool {
	return isGabEnabled() && gabdebug.gabStrAlloc > 0
}

func isGabStrHeaderAllocEnabled() bool {
	return isGabEnabled() && gabdebug.gabStrHeaderAlloc > 0
}

func isGabDebugStatEnabled() bool {
	return isGabEnabled() && gabdebug.gabDebugStat > 0
}

//go:linkname dbgReleaseCurrentGABBuffers
func dbgReleaseCurrentGABBuffers() {
	gp := getg()
	gp.ab.retire()
	gp.strAB.retire()
	gp.strHeaderAB.clear()
}

func shouldPauseGab() bool {
	return isGabStrAllocEnabled() &&
		uint64(atomic.Loaduintptr(&gabUsedBytes)) >= gabState.gabMemLimit &&
		gcController.heapGoal() >= uint64(gabUsePolicyThreshold)
}

// Notice the end of GC, safe place to perform global strategy adjustment.
// NOTE: pls do not add slow code here, since this method is called at the end of GC STW,
func gabNoteGCEnd() {
	if !gabPolicyEnabled {
		return
	}
	// if user set gabMemoryPercent to invalid option via dynconf, reset it to default
	if gabMemoryPercent > 100 || gabMemoryPercent < 0 {
		gabMemoryPercent = gabDefaultMemoryPercent
	}
	if gabMemoryPercent == 0 { // effectively disabled
		gabEnabled = false
		// Disable GAB allocation fastpath
		gabInliningEnabled = false
		return
	}
	// Use GAB pacer if GOGABOPT=gabPacer=1 is specified.
	if gabdebug.gabPacer == 1 {
		gabState.gabPacer()
	} else {
		gabState.oldStrategy()
	}
}
