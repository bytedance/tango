// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adaptive RunTime Scheduling

package runtime

import (
	"internal/runtime/atomic"
	"unsafe"
)

// adapt-G stands for goroutines under the Adaptive RunTime Scheduling flow,
// they will be featured a few dynamic attributes allowing the runtime schedule
// them in a more flexible way than normal, aiming to achieve better responsive
// time, lower memory footprint, lower GC pressure, etc..
//
// Currently, a goroutine will be made adapt-G if it's spawned by a Tango Poller
// goroutine (refer to tango/poll package) either directly or indirectly and
// GODEBUG=adapt=1 is specified at the same time. Eventually, some dynamic
// control strategies will be introduced to set the attribute more intelligently.
//
// The runtime has a two-level goroutine reusing mechanism now, freed goroutines
// are saved in several per-P and one global free lists. Adaptive runtime scheduling
// behaves in a similar way for the freed adapt-Gs, while maintain them in separate
// free lists, the two sets of free list are not isolated entirely to prevent the
// worst case that too many Gs are hold by one set but not reusable by the other,
// thus a normal freed G might be promoted to an adapt-G, and an adapt-G might be
// decayed to a normal one.

// The following constants could be declared as variables later given
// it's necessary to fine-tune them during runtime execution.
const (
	// max capacity of per-P free adapt-G list
	// usually, larger capacity reduces scheduling latency at a cost of more
	// memory consumed by stacks.
	_AdaptFreeGMax = 64

	// expected number of free adapt-G grabbed from global list
	_AdaptFreeGGrab = _AdaptFreeGMax >> 1

	// max stack size of a reusable adapt-G
	// goroutine having a larger stack size upon exit is decayed to
	// a normal one with its stack being freed.
	_AdaptGStackMax = stackMin << 5
)

var (
	// initial stack size of a newly-created adapt-G
	// for simplicity, we calculate the value upon adapt-G's exit, instead of
	// capturing its peak amid runtime execution.
	// it should be made a map using adapt-G's start pc as key, eventually
	_adaptInitStksize uintptr = stackMin
)

// adaptUpdateInitStksize updates the suggested stack size for adapt-G.
// TODO: it could be made more dynamic and smart to avoid unnecessary updating if,
// say, detecting _adaptInitStksize stays unchanged in the past N calls, we'll
// see whether it's worthy the complexity.
func adaptUpdateInitStksize(sz uintptr) {
	if !(sz > stackMin && sz <= _AdaptGStackMax) {
		return
	}
	old := atomic.Loaduintptr(&_adaptInitStksize)
	if sz > old {
		atomic.Casuintptr(&_adaptInitStksize, old, sz)
	}
}

// adaptInitStksize gets the current suggested stack size for adapt-G.
// the return value is expected to be in [_StackMin, _AdaptGStackMax]
func adaptInitStksize() int32 {
	return int32(atomic.Loaduintptr(&_adaptInitStksize))
}

// adaptPollEnable sets the current G to adaptive runtime scheduling mode.
//
//go:linkname adaptPollEnable internal/poll.runtime_tangoPollEnable
func adaptPollEnable() {
	// for now, the Gstack optimization is the only one that enables
	// a goroutine's adaptive scheduling mode.
	// we shall disable adaptSched's adjustment to initial stack size
	// if the new dynamic stack size adjustment introduced to 1.19
	// is present.
	if debug.adaptivestackstart == 0 {
		getg().adaptSched = artsOptEnabled(_AdaptOptGstack)
	}
	getg().adaptPoller = artsOptEnabled(_AdaptOptPoller)
}

// adaptGfput, adaptGfget and adaptGfpurge are in parallel with their non-adapt
// versions, but operate on separate free lists. For now, we try the best
// to make the adapt* versions strictly comply with their counterparts, until we
// are confident to move forward with any optimizations.

// adaptGfput caches an adapt-G to free list.
// If local list is too long, transfer a batch to the global list.
// TODO: put an upper limit to the number of adapt-Gs
func adaptGfput(_p_ *p, gp *g) {
	// better to clear all possible adapt flags on exit to ensure
	// consistent status among all free Gs.
	gp.adaptPoller = false
	gp.adaptSched = false

	stksize := gp.stack.hi - gp.stack.lo
	if stksize == fixedStack || stksize > _AdaptGStackMax {
		// decayed to a normal one if no stack expansion upon exit or
		// expanding too many times
		gfput(_p_, gp)
		return
	}
	adaptUpdateInitStksize(stksize)

	if readgstatus(gp) != _Gdead {
		throw("adaptGfput: bad status (not Gdead)")
	}

	_p_.gAdaptFree.push(gp)
	_p_.gAdaptFree.n++
	if _p_.gAdaptFree.n >= _AdaptFreeGMax {
		lock(&sched.gAdaptFree.lock)
		for _p_.gAdaptFree.n >= _AdaptFreeGGrab {
			_p_.gAdaptFree.n--
			gp = _p_.gAdaptFree.pop()
			sched.gAdaptFree.push(gp)
			sched.gAdaptFree.n++
		}
		unlock(&sched.gAdaptFree.lock)
	}
}

// adaptGfget gets an adapt-G from free list.
// If local list is empty, grab a batch from global list.
func adaptGfget(_p_ *p) *g {
retry:
	if _p_.gAdaptFree.empty() && !sched.gAdaptFree.empty() {
		lock(&sched.gAdaptFree.lock)
		// Move a batch of free Gs to the P.
		for _p_.gAdaptFree.n < _AdaptFreeGGrab {
			gp := sched.gAdaptFree.pop()
			if gp == nil {
				break
			}
			sched.gAdaptFree.n--
			_p_.gAdaptFree.push(gp)
			_p_.gAdaptFree.n++
		}
		unlock(&sched.gAdaptFree.lock)
		goto retry
	}
	gp := _p_.gAdaptFree.pop()
	if gp == nil {
		return nil
	}
	_p_.gAdaptFree.n--

	if raceenabled {
		racemalloc(unsafe.Pointer(gp.stack.lo), gp.stack.hi-gp.stack.lo)
	}
	if msanenabled {
		msanmalloc(unsafe.Pointer(gp.stack.lo), gp.stack.hi-gp.stack.lo)
	}
	return gp
}

// adaptGfpurge evicts all cached adapt-Gs from local free list to the global list.
func adaptGfpurge(_p_ *p) {
	lock(&sched.gAdaptFree.lock)
	for !_p_.gAdaptFree.empty() {
		gp := _p_.gAdaptFree.pop()
		_p_.gAdaptFree.n--
		sched.gAdaptFree.push(gp)
		sched.gAdaptFree.n++
	}
	unlock(&sched.gAdaptFree.lock)
}

// adaptSchedTrace prints the details of adapt-G's scheduling.
// It's for internal diagnostic and called in 'mark termination' only,
// the tracing is expected to work even the adaptive flow is NOT
// enabled.
func adaptSchedTrace() {
	var sbuf [24]byte
	printlock()
	print("adapt @", string(itoaDiv(sbuf[:], uint64(work.tSweepTerm-runtimeInitTime)/1e6, 3)), "s: ")
	print("allgs:", len(allgs), ", sched:[", sched.gFree.stack.size, ",", sched.gAdaptFree.n, "]")
	print(", [", int64(_adaptInitStksize), "], free:[")
	for i, _p_ := range allp {
		print(_p_.gFree.size, "/", _p_.gAdaptFree.size)
		if i != len(allp)-1 {
			print(", ")
		} else {
			print("]")
		}
	}
	print(", runqsize:[")
	for i, _p_ := range allp {
		print(_p_.runqtail - _p_.runqhead)
		if i != len(allp)-1 {
			print(", ")
		} else {
			print("]")
		}
	}
	print(", next:[")
	for i, _p_ := range allp {
		if _p_.runnext != 0 {
			print("1")
		} else {
			print("0")
		}
		if i != len(allp)-1 {
			print(", ")
		} else {
			print("]")
		}
	}
	print("\n")
	printunlock()
}
