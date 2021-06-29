// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/abi"
	"internal/bytealg"
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
	gabMaxAllocSize                 = abi.GabMaxAllocSize
	gabUsePolicyThreshold   uintptr = 1 << 25 // threshold that automatic policy starts to take effect, 32MB
	gabDefaultMemoryPercent         = 20
)

var (
	gabCapMax    uintptr = gabCapMaxDefault // default value of gab Max Capacity.
	gabUsedBytes uintptr                    // total consumed bytes
	gabVerbose   = false                    // print extra information
	// 'gabEnabled' control the runtime state of GAB feature,
	// mallocgc() will check this flag to determine if should goto fastpath.
	// on the other hand, 'runtime.debug.gab' represents user's intension
	// about whether GAB is expected to be enabled over all or not
	gabEnabled       = false
	gabMemoryPercent int32  // hard limit on total bytes used by GAB, 10% of memory target
	gabPolicyEnabled = true // control whether policy should take effect
)

var gabdebug struct {
	// Level of tracing for goroutine-allocation-buffer
	// 0: tracing disabled
	// 1: trace gab memory chunks
	// 2: trace mspan
	// 3: trace object, references
	trace int32
}

var gabvars = []dbgVar{
	{name: "gabPercent", value: &gabMemoryPercent},
	{name: "trace", value: &gabdebug.trace},
}

func gabinit() {
	gabMemoryPercent = gabDefaultMemoryPercent

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

	if gabMemoryPercent >= 100 || gabMemoryPercent < 0 {
		throw("Illegal gabMemoryPercent, should < 100 && >= 0")
	}

	if debug.gab == 1 || debug.gab > maxSmallSize || debug.gab < gabCapInitial {
		gabCapMax = gabCapMaxDefault
		gabPolicyEnabled = true
	} else {
		gabCapMax = uintptr(debug.gab >> gabCapShift << gabCapShift)
		gabPolicyEnabled = false
	}
	// default values
	gabEnabled = true
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
}

func (buf *gab) clear() {
	buf.base = nil
	buf.top = 0
	buf.end = 0
}

func (buf *gab) refill() {
	ncap := buf.nextCapacity()
	buf.base = mallocgc1(ncap, nil, true, true)
	buf.top = uintptr(buf.base)
	buf.end = buf.top + ncap

	if gabdebug.trace >= 1 {
		s := spanOf(buf.top)
		if s == nil || !s.isGab() {
			print("gab.refill(): span=", s, " is not for GAB allocation\n")
			throw("Invalid span")
		}
		print("Alloc gab:g=", hex(uintptr(unsafe.Pointer(getg()))), ", [", hex(buf.top), "~", hex(buf.end), ")\n")
	}
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
		return
	}
	// try to decide only if total heap retained bytes > gabCapMax
	memalloc := uintptr(gcController.heapLive.Load())
	if memalloc > gabUsePolicyThreshold {
		thresh := memalloc * uintptr(gabMemoryPercent) / 100
		gabEnabled = false
		if gabUsedBytes < thresh {
			if gabVerbose {
				println("Enable GAB: ", gabUsedBytes, "<", thresh)
			}
			gabEnabled = true
		}
	}
}
