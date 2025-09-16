// Copyright 2009 The Go Authors. All rights reserved.
// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Portions adapted from Go src/runtime/mprof.go.
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2025-09-16.

package runtime

import "unsafe"

// profileAllocGab is edited from profilealloc, it calls mProfMallocGab to profile
// the curg before switching to the systemstack.
func profileAllocGab(mp *m, x unsafe.Pointer, size uintptr, curg *g) {
	c := getMCache(mp)
	if c == nil {
		throw("profileAllocGab called without a P or outside bootstrapping")
	}
	c.nextSample = nextSample()
	mProfMallocGab(mp, x, size, curg)
}

// mProfMallocGab is edited from mProf_Malloc, it gets the caller stack of the curg.
func mProfMallocGab(mp *m, p unsafe.Pointer, size uintptr, curg *g) {
	if mp.profStack == nil {
		// mp.profStack is nil if we happen to sample an allocation during the
		// initialization of mp. This case is rare, so we just ignore such
		// allocations. Change MemProfileRate to 1 if you need to reproduce such
		// cases for testing purposes.
		return
	}
	// Only use the part of mp.profStack we need and ignore the extra space
	// reserved for delayed inline expansion with frame pointer unwinding.
	//
	var nstk int
	if curg == nil {
		// goroutine-allocation-buffer feature splits runtime.mallocgc into two calls
		// (but in-gab allocation will not trigger memory profiling)
		// so here we have to skip 6 callers instead of 5
		nstk = callers(6, mp.profStack[:debug.profstackdepth])
	} else {
		// When BalancedGC refills a buffer, it will switch to the systemstack to call mallocgc1.
		// This will cause the original user stack to be lost in mem profile.
		// So in this case, we need to get the stack of the user g to produce correct mem profile.
		nstk = gcallers(curg, 4, mp.profStack[:debug.profstackdepth])
	}

	index := (mProfCycle.read() + 2) % uint32(len(memRecord{}.future))

	b := stkbucket(memProfile, size, mp.profStack[:nstk], true)
	mr := b.mp()
	mpc := &mr.future[index]

	lock(&profMemFutureLock[index])
	mpc.allocs++
	mpc.alloc_bytes += size
	unlock(&profMemFutureLock[index])

	// Setprofilebucket locks a bunch of other mutexes, so we call it outside of
	// the profiler locks. This reduces potential contention and chances of
	// deadlocks. Since the object must be alive during the call to
	// mProf_Malloc, it's fine to do this non-atomically.
	systemstack(func() {
		setprofilebucket(p, b)
	})
}
