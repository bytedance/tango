// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstats

import (
	"net/http"
	"sort"
	"time"
)

type mmStat struct {
	// number of allocated and freed heap objects
	allocs, frees uint64
	// percentages of allocated and freed against all heap objects
	allocs_pct, frees_pct float64
}

type objStat struct {
	size   uint32
	scan   mmStat
	noscan mmStat
}

func (o *objStat) scanStats() (uint64, uint64, float64, float64) {
	return o.scan.allocs, o.scan.frees, o.scan.allocs_pct, o.scan.frees_pct
}

func (o *objStat) noScanStats() (uint64, uint64, float64, float64) {
	return o.noscan.allocs, o.noscan.frees, o.noscan.allocs_pct, o.noscan.frees_pct
}

func (o *objStat) addStats(sa, sf, nsa, nsf uint64) {
	o.scan.allocs += sa
	o.scan.frees += sf
	o.noscan.allocs += nsa
	o.noscan.frees += nsf
}

func (o *objStat) calcPcts(res *memStat) {
	if res.mallocs != 0 {
		o.scan.allocs_pct = float64(o.scan.allocs) / float64(res.mallocs) * 100.0
		o.noscan.allocs_pct = float64(o.noscan.allocs) / float64(res.mallocs) * 100.0
	}

	if res.frees != 0 {
		o.scan.frees_pct = float64(o.scan.frees) / float64(res.frees) * 100.0
		o.noscan.frees_pct = float64(o.noscan.frees) / float64(res.frees) * 100.0
	}
}

func (o *objStat) less(i *objStat) bool {
	if o.noscan.allocs != i.noscan.allocs {
		return o.noscan.allocs < i.noscan.allocs
	}
	if o.noscan.frees != i.noscan.frees {
		return o.noscan.frees < i.noscan.frees
	}
	if o.scan.allocs != i.scan.allocs {
		return o.scan.allocs < i.scan.allocs
	}
	if o.scan.frees != i.scan.frees {
		return o.scan.frees < i.scan.frees
	}
	return false
}

type memStat struct {
	// total number of allocations and frees
	mallocs, frees uint64

	// summary, per size group (reuse the 'no-scan' slots only)
	large objStat
	small objStat
	tiny  objStat

	// remained candidates for GAB allocator (reuse the 'no-scan' slots only)
	gab objStat

	// distrubution of small objects, per size class
	smalls []objStat
}

func sleep(r *http.Request, d time.Duration) {
	select {
	case <-time.After(d):
	case <-r.Context().Done():
	}
}

// offsets, please keep it line up with runtime.tangoReadMemStats
const (
	totalMallocs int = iota
	totalFrees
	largeNoScanAllocs
	largeNoScanFrees
	largeScanAllocs
	largeScanFrees
	tinyAllocs
	tinyFrees
)

// collectmemStats collecting memory statistics using an extended runtime API
func collectMemStats(r *http.Request, sec int64, sorted bool) *memStat {
	sizes, counts := tangoReadMemStats()

	numSizes := tangoNumSizes()

	if len(sizes) != numSizes {
		panic("Unexpected return value of tangoNumSizes()")
	}

	if sec > 0 {
		sleep(r, time.Duration(sec)*time.Second)
		_, counts1 := tangoReadMemStats()
		for i, c := range counts {
			counts1[i] -= c
		}
		counts = counts1[:]
	}

	// post-processing
	p := make([]objStat, numSizes, numSizes)
	res := &memStat{}

	// total
	res.mallocs, res.frees = counts[totalMallocs], counts[totalFrees]

	// large objects
	res.large.addStats(0, 0, counts[largeScanAllocs], counts[largeScanFrees])
	res.large.addStats(0, 0, counts[largeNoScanAllocs], counts[largeNoScanFrees])

	// tiny objects
	res.tiny.addStats(0, 0, counts[tinyAllocs], counts[tinyFrees])

	// per-class objects
	bySizeNoScan := tinyFrees + 1
	bySizeScan := bySizeNoScan + numSizes*2
	for i := 1; i < numSizes; i++ {
		off := i * 2
		ssa, ssf := counts[bySizeScan+off], counts[bySizeScan+off+1]
		sna, snf := counts[bySizeNoScan+off], counts[bySizeNoScan+off+1]

		// small objects
		p[i].size = sizes[i]
		p[i].addStats(ssa, ssf, sna, snf)
		p[i].calcPcts(res)
		res.small.addStats(0, 0, ssa, ssf)
		res.small.addStats(0, 0, sna, snf)

		// gab candidates
		if p[i].size <= gabMaxAllocSize() {
			res.gab.addStats(0, 0, sna, snf)
		}
	}

	res.large.calcPcts(res)
	res.tiny.calcPcts(res)
	res.small.calcPcts(res)
	res.gab.calcPcts(res)

	// p[0] designates large objects, so drop it for stats. of small objects
	p = p[1:]
	if sorted {
		sort.Slice(p, func(i, j int) bool {
			return !p[i].less(&p[j])
		})
	}

	res.smalls = p[:]
	return res
}
