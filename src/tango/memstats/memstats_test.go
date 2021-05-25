// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstats

import (
	"math/rand"
	"net/http"
	"testing"
)

var sink []interface{}

func TestCollect(t *testing.T) {
	req := &http.Request{}
	numThres := 4096
	sizThres := 64 * 1024
	dur := int64(10)

	if testing.Short() {
		numThres = 256
		sizThres = 16 * 1024
		dur = int64(2)
	}

	go func() {
		// try to avoid large memory footprint.
		// the estimated heap allocation is about 128M (and 2M for short mode),
		// but the memory obtained from OS might be several times of that.
		for i := 0; i < numThres; i++ {
			sink = append(sink, make([]byte, rand.Intn(sizThres)))
			if i%5 == 0 {
				sink[i] = nil
			}
		}
	}()

	res := collectMemStats(req, dur, false)

	// sanity checking the various statistics
	sm0 := res.small.scan.allocs + res.small.noscan.allocs
	sf0 := res.small.scan.frees + res.small.noscan.frees

	tm := sm0
	tm += res.large.scan.allocs + res.large.noscan.allocs
	tm += res.tiny.scan.allocs + res.tiny.noscan.allocs

	t.Logf("Total mallocs calculated: %d\n", tm)
	if tm != res.mallocs {
		t.Errorf("Total mallocs mismatch, got: %d, want: %d\n", tm, res.mallocs)
	}

	tf := sf0
	tf += res.large.scan.frees + res.large.noscan.frees
	tf += res.tiny.scan.frees + res.tiny.noscan.frees

	t.Logf("Total frees calculated: %d\n", tf)
	if tf != res.frees {
		t.Errorf("Total frees mismatch, got: %d, want: %d\n", tf, res.frees)
	}

	sm := uint64(0)
	sf := uint64(0)
	for i := 0; i < len(res.smalls); i++ {
		sm += res.smalls[i].scan.allocs + res.smalls[i].noscan.allocs
		sf += res.smalls[i].scan.frees + res.smalls[i].noscan.frees
	}
	t.Logf("Small mallocs/frees: %d, %d\n", sm, sf)
	if sm != sm0 {
		t.Errorf("Small mallocs mismatch, got: %d, want: %d\n", sm, sm0)
	}
	if sf != sf0 {
		t.Errorf("Small frees mismatch, got: %d, want: %d\n", sf, sf0)
	}
}
