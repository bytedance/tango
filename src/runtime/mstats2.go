// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Memory statistics, with additional information for the "memstats" profiling

package runtime

import (
	"internal/runtime/gc"
	_ "unsafe"
)

var (
	memstats2 mstats2
)

// mstats2 records memory statistics that are not covered by mstats.
type mstats2 struct {
	// allocator stats. of all large objects and large no-scan objects,
	// stats. of large scan objects will be calculated on the fly.
	large_allocs        uint64
	large_frees         uint64
	large_noscan_allocs uint64
	large_noscan_frees  uint64
	// allocator stats. of all tiny objects
	tiny_allocs uint64

	// allocator stats. of all small no-scan objects per size class
	by_size_noscan [gc.NumSizeClasses]struct {
		nmalloc uint64
		nfree   uint64
	}

	// allocator stats. of all small scan & no-scan objects per size class
	by_size [gc.NumSizeClasses]struct {
		nmalloc uint64
		nfree   uint64
	}
}

type heapStatsDeltaEx struct {
	// Additional allocator stats. for the 'memstats' profiling
	largeNoScanAllocCount uint64                    // number of large object allocations (no-scan)
	largeNoScanFreeCount  uint64                    // number of frees for large objects (>maxSmallSize, no-scan)
	smallNoScanAllocCount [gc.NumSizeClasses]uint64 // number of allocs for small objects (no-scan)
	smallNoScanFreeCount  [gc.NumSizeClasses]uint64 // number of frees for small objects (<=maxSmallSize, no-scan)
}

func (a *heapStatsDeltaEx) merge(b *heapStatsDeltaEx) {
	a.largeNoScanAllocCount += b.largeNoScanAllocCount
	a.largeNoScanFreeCount += b.largeNoScanFreeCount
	for i := range b.smallNoScanAllocCount {
		a.smallNoScanAllocCount[i] += b.smallNoScanAllocCount[i]
	}
	for i := range b.smallNoScanFreeCount {
		a.smallNoScanFreeCount[i] += b.smallNoScanFreeCount[i]
	}
}

// tangoNumSizes returns the number of runtime allocator's size classes
//
//go:linkname tangoNumSizes
func tangoNumSizes() int {
	return gc.NumSizeClasses
}

// tangoNumCounters returns the number of counters of heap object allocation/free
func tangoNumCounters() int {
	return 8 + gc.NumSizeClasses*4
}

// Updates the memstats2 structure.
// Should be called by readmemstats_m only.
//
//go:nowritebarrier
func updatememstats2(consStats *heapStatsDelta) {
	assertWorldStopped()
	for i := 0; i < gc.NumSizeClasses; i++ {
		memstats2.by_size_noscan[i].nmalloc = 0
		memstats2.by_size_noscan[i].nfree = 0
		memstats2.by_size[i].nmalloc = 0
		memstats2.by_size[i].nfree = 0
	}

	// Collect additional large/tiny allocation stats. for the 'memstats' profiling
	memstats2.large_allocs = consStats.largeAllocCount
	memstats2.large_frees = consStats.largeFreeCount
	memstats2.large_noscan_allocs = consStats.ext.largeNoScanAllocCount
	memstats2.large_noscan_frees = consStats.ext.largeNoScanFreeCount
	memstats2.tiny_allocs = consStats.tinyAllocCount

	// Collect per-sizeclass no-scan stats.
	for i := 0; i < gc.NumSizeClasses; i++ {
		memstats2.by_size_noscan[i].nmalloc = consStats.ext.smallNoScanAllocCount[i]
		memstats2.by_size_noscan[i].nfree = consStats.ext.smallNoScanFreeCount[i]
		memstats2.by_size[i].nmalloc = consStats.smallAllocCount[i]
		memstats2.by_size[i].nfree = consStats.smallFreeCount[i]
	}
}

// tangoReadMemStats collects the statistics of heap object allocations and frees
//
// It's supposed to be used by package tango/memstats only, no public API
// is exported, thereby both the runtime and tango/memstats packages are responsible
// to ensuring the compliance of return values
//
//go:linkname tangoReadMemStats
func tangoReadMemStats() (sizes []uint32, counts []uint64) {
	numSizes := tangoNumSizes()
	numCounts := tangoNumCounters()

	sizes = make([]uint32, numSizes, numSizes)
	counts = make([]uint64, numCounts, numCounts)

	stw := stopTheWorld(stwMemStats)
	var m MemStats
	systemstack(func() {
		readmemstats_m(&m)
	})

	counts[0] = m.Mallocs
	counts[1] = m.Frees
	counts[2] = memstats2.large_noscan_allocs
	counts[3] = memstats2.large_noscan_frees
	counts[4] = memstats2.large_allocs - memstats2.large_noscan_allocs
	counts[5] = memstats2.large_frees - memstats2.large_noscan_frees
	counts[6] = memstats2.tiny_allocs
	counts[7] = memstats2.tiny_allocs

	for i := 0; i < numSizes; i++ {
		sizes[i] = uint32(gc.SizeClassToSize[i])
	}

	nspos := 8
	spos := nspos + numSizes*2
	for i := 0; i < numSizes; i++ {
		off := i * 2
		counts[nspos+off] = memstats2.by_size_noscan[i].nmalloc
		counts[nspos+off+1] = memstats2.by_size_noscan[i].nfree
		counts[spos+off] = memstats2.by_size[i].nmalloc - counts[nspos+off]
		counts[spos+off+1] = memstats2.by_size[i].nfree - counts[nspos+off+1]
	}
	startTheWorld(stw)
	return
}
