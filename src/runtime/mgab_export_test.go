// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Export guts for testing.

package runtime

import (
	"internal/goarch"
	"unsafe"
)

// GetGABObjectSize return the size of GAB-allocated object.
func GetGABObjectSize(addr uintptr) (uintptr, bool) {
	s := spanOf(addr)
	if s == nil || !s.isGab() {
		println(hex(addr), "is not a heap address.")
		return 0, false
	}
	stw := stopTheWorld(stwVerifyGABs)
	makeGABParsable()
	gabStart := s.base() + s.objIndex(addr)*s.elemsize
	_, size := gabFindBlockFast(addr, gabStart)
	startTheWorld(stw)
	return size, true
}

// If a GAB is attached to a goroutine, it is active.
func isActiveGAB(start uintptr) bool {
	isActive := false
	for _, gp := range allgs {
		if gp != nil && uintptr(gp.ab.base) == start {
			isActive = true
			break
		}
	}
	if !isActive {
		for mp := allm; mp != nil; mp = mp.alllink {
			if mp.g0 != nil && uintptr(mp.g0.ab.base) == start {
				isActive = true
				break
			}
		}
	}
	return isActive
}

// Iterate from the beginning to the end of a GAB.
func verifyGAB(start, end uintptr) bool {
	s := spanOf(start)
	var ptr uintptr
	for ptr = start; ptr < end; {
		// Find the beginning of GAB objects
		index := (ptr - s.startAddr) / goarch.PtrSize
		if !s.isObjStart(index) {
			println(hex(ptr), "are expected to be the beginning of an object")
			return false
		}
		_, size := gabFindBlockFast(ptr, start)
		if size == 0 {
			println("Object", hex(ptr), "should not be zero")
			return false
		}
		// The body of an object should not be marked
		objBody := ptr + goarch.PtrSize
		objEnd := ptr + size
		for p := objBody; p < objEnd; p += goarch.PtrSize {
			idx := (p - s.startAddr) / goarch.PtrSize
			if s.isObjStart(idx) {
				print("Object from ", hex(objBody), "~", hex(objEnd), " should not be the beginning of an object.")
				return false
			}
		}
		ptr += size
	}
	return true
}

func VerifyGABs() bool {
	stw := stopTheWorld(stwVerifyGABs)
	makeGABParsable()
	for _, s := range mheap_.allspans {
		if !s.isGab() || s.allocCount == 0 {
			continue
		}
		base := s.base()
		elementSize := s.elemsize
		for i := uintptr(0); i < uintptr(s.nelems); i++ {
			if i < uintptr(s.freeindex) || (i >= uintptr(s.freeindex) && s.allocBitsForIndex(i).isMarked()) {
				start := base + i*elementSize
				end := start + elementSize
				print("Verify GAB start=", hex(start), ", end=", hex(end), ", size=", elementSize, "\n")
				if !verifyGAB(start, end) {
					startTheWorld(stw)
					return false
				}
			}
		}
	}
	startTheWorld(stw)
	return true
}

func VerifyObjStartBits(ptrs []unsafe.Pointer, objSize uintptr) bool {
	stw := stopTheWorld(stwVerifyGABs)
	makeGABParsable()
	for i := 0; i < len(ptrs); i++ {
		p := uintptr(ptrs[i])
		s := spanOf(p)
		gabStart := s.base() + s.objIndex(p)*s.elemsize
		_, size := gabFindBlockFast(p, gabStart)
		if size != objSize {
			startTheWorld(stw)
			return false
		}
	}
	startTheWorld(stw)
	return true
}

func makeGABParsable() {
	lock(&allglock)
	for _, gp := range allgs {
		gp.ab.makeParsable()
	}
	unlock(&allglock)

	for mp := allm; mp != nil; mp = mp.alllink {
		mp.g0.ab.makeParsable()
	}
}

func GabMarked() uint64 {
	return work.gabMarked
}

func LiveGabSize() uintptr {
	stw := stopTheWorld(stwVerifyGABs)
	var size uintptr
	for _, s := range mheap_.allspans {
		if !s.isGab() || s.allocCount == 0 {
			continue
		}
		for i := uintptr(0); i < uintptr(s.nelems); i++ {
			if i < uintptr(s.freeindex) || s.allocBitsForIndex(i).isMarked() {
				size += s.elemsize
			}
		}
	}
	startTheWorld(stw)
	return size
}

// Switch from balanced GC to GAB during STW.
func BalancedGC2GAB() {
	if debug.gab != 2 {
		throw("Balanced GC is expected to be enabled.")
	}
	stw := stopTheWorld(stwVerifyGABs)
	debug.gab = 1
	startTheWorld(stw)
}

// Return off-heap memory usage of GAB metadata
func GetGabSysMemUsage() uint64 {
	return memstats.gab_sys.load()
}
