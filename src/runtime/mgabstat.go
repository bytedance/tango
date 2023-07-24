// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

var gabgcstats gabGCStats

type gabGCStats struct {
	// nano time timestamp of balanced GC
	balancedGCStartNs int64
	balancedGCEndNs   int64
	// nano time timestamp of starting GAB evacuation
	evacStartNs int64
	evacEndNs   int64
	// total number of references enqueued in this GC cycle
	refsEnqueued uint64
	// total number of references processed in this GC cycle
	refsProcessed uint64
	// number of copied objects
	objCopied uint64
	// total bytes of copied objects
	bytesCopied uint64
	// references get updated with a forwarded object address
	refsUpdated uint64
	// number of refs skipped at evacuation
	refsSkipped uint64
	// live GAB before evacuation
	gabUsedBytesBeforeEvac uintptr
	// marked GAB objects in bytes
	gabMarkedBytes uintptr
	// evacuated GABs in bytes
	gabEvacuatedBytes uintptr
	// rescanned goroutines
	rescannedG uint64
	// nano time timestamp of rescanning goroutines
	rescanStartNs int64
	rescanEndNs   int64
}

func (stat *gabGCStats) print() {
	var sbuf [24]byte
	printlock()
	print("[Balanced GC: gc ", memstats.numbalancedgc, " ")
	print("@", string(fmtNSAsMS(sbuf[:], uint64(stat.balancedGCEndNs-stat.balancedGCStartNs))), " ms: ")
	print("rescanning @", string(fmtNSAsMS(sbuf[:], uint64(stat.rescanEndNs-stat.rescanStartNs))), " ms: ", stat.rescannedG, " goroutines; ")
	print("evacuation @", string(fmtNSAsMS(sbuf[:], uint64(stat.evacEndNs-stat.evacStartNs))), " ms: ")
	print(stat.gabUsedBytesBeforeEvac>>10, " KB live, ", stat.gabMarkedBytes>>10, " KB marked, ", stat.bytesCopied>>10, " KB copied; ")
	print(stat.refsProcessed, " references processed = ", stat.objCopied, " objects copied + ", stat.refsUpdated, " references updated + ", stat.refsSkipped, " references skipped; ")
	print("evacuated GABs ", stat.gabEvacuatedBytes>>10, " KB; ")
	print("off-heap memory ", memstats.gab_sys.load(), " B; ")
	print("reason: ", gabState.gabEvacReason)
	println("]")
	printunlock()
}

// check validity at the end of GAB gc
func (stat *gabGCStats) check() {
	if stat.refsEnqueued != stat.refsProcessed {
		throw("Inconsistent refs enqueued-vs-processed")
	}
	if stat.refsProcessed != (stat.objCopied + stat.refsUpdated + stat.refsSkipped) {
		throw("Inconsistent refs processed-vs-<parts>")
	}
}

func (stat *gabGCStats) reset() {
	memclrNoHeapPointers(unsafe.Pointer(stat), unsafe.Sizeof(gabgcstats))
}
