// Copyright 2022 The Go Authors. All rights reserved.
// Copyright (c) 2021 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2021-05-25.

// This file lives in the runtime package
// so we can get access to the runtime guts.
// The rest of the implementation of this test is in align_test.go.

package runtime

import "unsafe"

// AtomicFields is the set of fields on which we perform 64-bit atomic
// operations (all the *64 operations in internal/runtime/atomic).
var AtomicFields = []uintptr{
	unsafe.Offsetof(m{}.procid),
	unsafe.Offsetof(profBuf{}.overflow),
	unsafe.Offsetof(profBuf{}.overflowTime),
	unsafe.Offsetof(heapStatsDelta{}.tinyAllocCount),
	unsafe.Offsetof(heapStatsDelta{}.smallAllocCount),
	unsafe.Offsetof(heapStatsDelta{}.smallFreeCount),
	unsafe.Offsetof(heapStatsDelta{}.largeAlloc),
	unsafe.Offsetof(heapStatsDelta{}.largeAllocCount),
	unsafe.Offsetof(heapStatsDelta{}.largeFree),
	unsafe.Offsetof(heapStatsDelta{}.largeFreeCount),
	unsafe.Offsetof(heapStatsDelta{}.committed),
	unsafe.Offsetof(heapStatsDelta{}.released),
	unsafe.Offsetof(heapStatsDelta{}.inHeap),
	unsafe.Offsetof(heapStatsDelta{}.inStacks),
	unsafe.Offsetof(heapStatsDelta{}.inWorkBufs),
	unsafe.Offsetof(heapStatsDelta{}.ext),
	unsafe.Offsetof(heapStatsDeltaEx{}.largeNoScanAllocCount),
	unsafe.Offsetof(heapStatsDeltaEx{}.largeNoScanFreeCount),
	unsafe.Offsetof(heapStatsDeltaEx{}.smallNoScanAllocCount),
	unsafe.Offsetof(heapStatsDeltaEx{}.smallNoScanFreeCount),
	unsafe.Offsetof(lfnode{}.next),
	unsafe.Offsetof(mstats{}.last_gc_nanotime),
	unsafe.Offsetof(mstats{}.last_gc_unix),
	unsafe.Offsetof(workType{}.bytesMarked),
	unsafe.Offsetof(workType{}.gabMarked),
	unsafe.Offsetof(gabGCStats{}.refsSkipped),
	unsafe.Offsetof(gabGCStats{}.refsUpdated),
	unsafe.Offsetof(gabGCStats{}.objCopied),
	unsafe.Offsetof(gabGCStats{}.bytesCopied),
	unsafe.Offsetof(gabGCStats{}.rescannedG),
	unsafe.Offsetof(gabGCStats{}.refsEnqueued),
	unsafe.Offsetof(gabGCStats{}.refsProcessed),
}

// AtomicVariables is the set of global variables on which we perform
// 64-bit atomic operations.
var AtomicVariables = []unsafe.Pointer{
	unsafe.Pointer(&ncgocall),
	unsafe.Pointer(&test_z64),
	unsafe.Pointer(&blockprofilerate),
	unsafe.Pointer(&mutexprofilerate),
	unsafe.Pointer(&gcController),
	unsafe.Pointer(&memstats),
	unsafe.Pointer(&sched),
	unsafe.Pointer(&ticks),
	unsafe.Pointer(&work),
	unsafe.Pointer(&gabgcstats),
}
