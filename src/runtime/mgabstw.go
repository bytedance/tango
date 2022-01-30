// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && (linux || darwin)
// +build amd64 arm64
// +build linux darwin

package runtime

import (
	"internal/goarch"
	"internal/runtime/atomic"
	"internal/runtime/gc"
	"internal/runtime/sys"
	"unsafe"
)

const (
	gabStwChunkLen      = 1 << 18 // 256K
	gabStwChunkCap      = gc.MaxSmallSize
	gabStwChunkTotalCap = gabStwChunkLen * gabStwChunkCap
)

var (
	gabStwChunks      [gabStwChunkLen]unsafe.Pointer
	gabStwInitDone    bool
	gabStwChunksAlloc int
	gabStwChunksUse   int
	// local to guard initialization status
	gabStwInitLock mutex
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
	ab.fillRemaining()
	ab.base = 0
	ab.top = 0
	ab.end = 0
	ab.cindex = 0
	ab.span = nil
}

//go:nowritebarrier
func (ab *stwgab) stwMalloc(size uintptr) unsafe.Pointer {
	assertWorldStopped()

	needfill := false
	if ab.base == 0 {
		needfill = true
	} else if ab.remaining() < size {
		ab.fillRemaining()
		needfill = true
	}
	if needfill {
		base := uintptr(gabStwChunks[ab.cindex])
		if base == 0 {
			throw("Survivor GAB is not allocated")
		}
		ab.base = base
		ab.top = base
		ab.end = ab.top + gabStwChunkCap
		ab.span = spanOf(base)
		ab.cindex++
		gabStwChunksUse++
	}

	x := ab.top
	ab.top += size
	return unsafe.Pointer(x)
}

func (ab *stwgab) remaining() uintptr {
	return ab.end - ab.top
}

func (ab *stwgab) fillRemaining() {
	rem := ab.remaining()
	if rem == 0 {
		return
	}
	if rem < goarch.PtrSize {
		throw("Too few remaining bytes to fill")
	}
	startObj(ab.top, ab.span)
}

func gabgcinit() {
	p := persistentalloc(unsafe.Sizeof(stwgab{}), goarch.PtrSize, &memstats.gab_sys)
	sgab = (*stwgab)(p)
	lockInit(&gabStwInitLock, lockRankMheap)
}

func gabInitStwChunks(totalSize uintptr) {
	if gcphase == _GCmarktermination {
		throw("mallocgc() is invoked during GC mark termination")
	}
	lockWithRank(&gabStwInitLock, lockRankMheap)
	if gabStwInitDone {
		unlockWithRank(&gabStwInitLock)
		return
	}

	if totalSize > gabStwChunkTotalCap {
		throw("Exceeding the max size of survivor GABs")
	}

	mp := acquirem()
	if mp.mallocing != 0 {
		throw("malloc deadlock")
	}
	if mp.gsignal == getg() {
		throw("malloc during signal")
	}
	mp.mallocing = 1
	c := getMCache(mp)
	if c == nil {
		throw("mallocgc called without a P or outside bootstrapping")
	}

	needzero := true
	noscan := true
	var span *mspan
	var nextIndex int
	size := uintptr(gabStwChunkCap)
	allocDone := uintptr(0)

	for {
		var x unsafe.Pointer
		if allocDone >= totalSize {
			break
		}
		if size <= gc.MaxSmallSize {
			if noscan && size < maxTinySize {
				throw("Should not allocate tiny objects")
			} else {
				var sizeclass uint8
				if size <= gc.SmallSizeMax-8 {
					sizeclass = gc.SizeToSizeClass8[divRoundUp(size, gc.SmallSizeDiv)]
				} else {
					sizeclass = gc.SizeToSizeClass128[divRoundUp(size-gc.SmallSizeMax, gc.LargeSizeDiv)]
				}
				size = uintptr(gc.SizeClassToSize[sizeclass])
				spc := makeSpanClass(sizeclass, noscan, true)
				atomic.Xadduintptr(&gabUsedBytes, size)
				span = c.alloc[spc]
				v := nextFreeFast(span)
				if v == 0 {
					v, span, _ = c.nextFree(spc)
				}
				x = unsafe.Pointer(v)
				if needzero && span.needzero != 0 {
					memclrNoHeapPointers(unsafe.Pointer(v), size)
				}
			}
			publicationBarrier()
			if gcphase != _GCoff {
				gcmarknewobject(span, uintptr(x))
			}
			gabStwChunks[nextIndex] = x
			nextIndex++
			allocDone += size
			if nextIndex > gabStwChunkLen {
				throw("Exceeding the max number of survivor GABs")
			}
		} else {
			throw("Should allocate small objects")
		}
	}

	gabStwChunksAlloc = nextIndex
	mp.mallocing = 0
	releasem(mp)
	gabStwInitDone = true
	unlockWithRank(&gabStwInitLock)
	if gabdebug.trace >= 1 {
		print("Allocating survivor GABs, size ", totalSize>>10, " KB, chunk size ", gabStwChunkCap, " B, # chunks ", gabStwChunksAlloc, "\n")
	}
}

//go:nowritebarrierrec
func gabClearStwChunks() {
	lockWithRank(&gabStwInitLock, lockRankMheap)
	for i := 0; i < gabStwChunksAlloc; i++ {
		atomic.StorepNoWB(unsafe.Pointer(&gabStwChunks[i]), nil)
	}
	sgab.clear()
	gabStwInitDone = false
	gabStwChunksAlloc = 0
	gabStwChunksUse = 0
	unlockWithRank(&gabStwInitLock)
}
