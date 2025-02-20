// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "internal/strconv"

const (
	// GAB evacuation reasons
	gabEvacThreshold = "threshold"
	gabEvacPeriodic  = "periodic"
)

type gabAllocState struct {
	// curGabMarked is total size of GABs that are marked alive in current GC cycle.
	// If GAB evacuation is performed, the size of survivor GABs is included.
	curGabMarked uint64
	// lastGabLive is the total size of GABs that are alive after GC sweeping.
	// If GAB evacuation is performed, the size of evacuated GABs is included.
	lastGabLive   uint64
	curHeapMarked uint64
	lastHeapLive  uint64
	// curHeapUsed is the heap size at the very beginning of mark termination.
	curHeapUsed uint64
	// curGabUsed is the GAB size at the very beginning of mark termination.
	curGabUsed uint64
	// gabEvacuated is the size of GABs that are evacuated in current GC cycle.
	// These GABs can be freed, since live objects are evacuated to survivor GABs.
	gabEvacuated uint64

	// gabRatio is the estimated percentage of GAB objects are allocated in
	// a GC cycle. gabRatio are expected to be steady when the program is
	// steady. Similar to GC pacer, a PI controller is introduced to smooth noise.
	gabRatio      float64
	gabRatioError float64
	// Total GAB objects that are estimated to be allocated in the next GC cycle.
	// gabRunway is approximated by using gabRatio times total size of heap
	// objects that are allocated in the next GC cycle.
	gabRunway uint64
	// gabMemLimit is the memory limit of GABs. It is the minimal value of 20% of
	// heapGoal or 10% of the pod memory.
	gabMemLimit     uint64
	gabMemHardLimit uint64
	isEvacuation    bool
	gabEvacReason   string

	gabAllocController piController
}

var gabState gabAllocState

func (gs *gabAllocState) init() {
	// Get pod memory limit.
	if mem := gogetenv("MY_MEM_LIMIT"); mem != "" {
		if memLimit, err := strconv.Atoi(mem); (err == nil) && memLimit > 0 {
			gs.gabMemHardLimit = uint64(memLimit) * gabDefaultPodMemoryPercent / 100
		}
	}

	gs.gabAllocController = piController{
		kp:  0.95,
		ti:  1,
		tt:  1000,
		min: 0,
		max: 100,
	}
}

func (gs *gabAllocState) updateGabEvacuated(gabEvacuated uint64) {
	gs.gabEvacuated = gabEvacuated
}

func (gs *gabAllocState) endCycle() {
	// heap1 is the total size of live heap objects after GC marking terminates.
	// Why not using gcControllerState.heapLive?
	// It has been reset to bytesMarked before this call.
	gs.curHeapUsed = work.heap1
	gs.curGabUsed = uint64(gabUsedBytes)
	gs.curGabMarked = work.gabMarked
	gs.curHeapMarked = work.bytesMarked
}

func (gs *gabAllocState) gabPacer() {
	assertWorldStopped()
	numgc := memstats.numgc
	gs.isEvacuation = false
	// gabLive is the total size of GABs that are alive at the
	// very beginning of the next GC cycle. Since GC sweeping
	// is performed concurrently, we are unable to know the
	// precise GAB size at that time. Consequently, we use
	// gabMarked as an approximation.
	// gabLive = live GABs + survivor GABs - evacuated GABs
	// curGabMarked is total size of GABs that are marked alive
	// during current GC cycle. Evacuated GABs that are marked
	// but are will be reclaimed later during GAB evacuation,
	// which are freed in the current GC cycle.
	// Note that curGabMarked includes live GABs and survivor
	// GABs, because they are allocated before mark termination
	// and are marked alive as new objects.
	gabLive := gs.curGabMarked - gs.gabEvacuated
	heapLive := gs.curHeapMarked - gs.gabEvacuated

	// Determine the memory limit of GABs
	heapGoal := gcController.heapGoal()
	// GAB memory limit takes effect only when heap usage exceeds the gabUsePolicyThreshold
	if heapGoal > uint64(gabUsePolicyThreshold) {
		enableGab := false
		// The hard memory limit of GABs is 10% of the pod memory.
		// The memory limit of GABs is minimal value of hard memory limit and 20% of heap goal.
		thresh := heapGoal * uint64(gabMemoryPercent) / 100
		if gs.gabMemHardLimit != 0 && gs.gabMemHardLimit < thresh {
			gs.gabMemLimit = gs.gabMemHardLimit
		} else {
			gs.gabMemLimit = thresh
		}
		var curGabRatio float64
		// It is possible that GAB memory shrinks or remains the same between recent two GCs,
		// e.g., GAB allocation turned off in the last GC cycle. If it happens, we use the
		// existing gabRatio.
		if gs.curGabUsed > gs.lastGabLive && gs.curHeapUsed > gs.lastHeapLive {
			gabAlloc := gs.curGabUsed - gs.lastGabLive
			heapAlloc := gs.curHeapUsed - gs.lastHeapLive
			curGabRatio = float64(gabAlloc) / float64(heapAlloc) * 100
		} else {
			curGabRatio = float64(gs.curGabUsed) / float64(gs.curHeapUsed) * 100
		}
		// Use a PI controller to smooth noise
		var ok bool
		oldGabRatio := gs.gabRatio
		gs.gabRatio, ok = gs.gabAllocController.next(oldGabRatio, curGabRatio, 1)
		if !ok {
			gs.gabRatio = 0
		}
		gs.gabRatioError = curGabRatio - oldGabRatio
		if gabdebug.trace >= 1 {
			print("[Balanced GC pacer: gc ", numgc, ": estimated ", oldGabRatio, ", actual ",
				curGabRatio, ", error ", gs.gabRatioError, ", next", gs.gabRatio, "]\n")
		}

		// Estimate total size of GAB objects allocated in next GC cycle
		nextHeapAlloc := heapGoal - heapLive
		gs.gabRunway = uint64(float64(nextHeapAlloc) * gs.gabRatio / 100)
		estimatedGabUsed := gabLive + gs.gabRunway
		if estimatedGabUsed < gs.gabMemLimit {
			enableGab = true
		} else {
			gs.gabEvacReason = gabEvacThreshold
			gs.isEvacuation = true
		}
		gabEnabled = enableGab
		if gabdebug.trace >= 1 {
			var s string
			if gabEnabled {
				s = "GAB enabled"
			} else {
				s = "GAB disabled"
			}
			print("[Balanced GC pacer: gc ", numgc, ": GAB live ", gs.lastGabLive>>10,
				" KB, GAB runway ", gs.gabRunway>>10, " KB, total ", estimatedGabUsed>>10,
				" KB, GAB threshold ", gs.gabMemLimit>>10, " KB, heap goal ", heapGoal>>10, " KB, ", s, "]\n")
		}
	}
	gs.lastGabLive = gabLive
	gs.lastHeapLive = heapLive
	gs.gabEvacuated = 0

	// Decide enable GAB allocation fastpath or not
	gabInliningEnabled = gabEnabled && !isEvacEnabled()

	// Periodic GAB evacuation
	if isEvacEnabled() && gabdebug.evacuation != 0 && int32(memstats.numgc+1)%gabdebug.evacuation == 0 {
		gs.gabEvacReason = gabEvacPeriodic
		gs.isEvacuation = true
	}
}

func (gs *gabAllocState) oldStrategy() {
	assertWorldStopped()
	gs.isEvacuation = false
	heapGoal := gcController.heapGoal()
	// try to decide only if total heap retained bytes > gabCapMax
	if heapGoal > uint64(gabUsePolicyThreshold) {
		enableGab := false
		// If either SetMaxHeap is enabled or gabMemoryPercent is explicitly specified,
		// use heapGoal to determine the GAB threshold.
		// If not, the GAB threshold is the min(heapGoal * gabMemoryPercent, gabMemHardLimit).
		thresh := heapGoal * uint64(gabMemoryPercent) / 100
		if !setMaxHeapEnabled() && gabMemoryPercent == gabDefaultMemoryPercent &&
			gs.gabMemHardLimit != 0 && gs.gabMemHardLimit < thresh {
			gs.gabMemLimit = gs.gabMemHardLimit
		} else {
			gs.gabMemLimit = thresh
		}
		gabMarked := work.gabMarked
		if gabMarked < gs.gabMemLimit {
			if gabdebug.trace >= 1 {
				print("[Enable GAB: GAB live ", gabMarked>>10, " KB, GAB threshold ", gs.gabMemLimit>>10, " KB]\n")
			}
			enableGab = true
		} else {
			gs.gabEvacReason = gabEvacThreshold
			gs.isEvacuation = true
			if gabdebug.trace >= 1 {
				print("[Disable GAB: GAB live ", gabMarked>>10, " KB, GAB threshold ", gs.gabMemLimit>>10, " KB]\n")
			}
		}
		gabEnabled = enableGab
	}

	// Decide enable GAB allocation fastpath or not
	gabInliningEnabled = gabEnabled && !isEvacEnabled()

	// Periodic GAB evacuation
	if isEvacEnabled() && gabdebug.evacuation != 0 && int32(memstats.numgc+1)%gabdebug.evacuation == 0 {
		gs.gabEvacReason = gabEvacPeriodic
		gs.isEvacuation = true
	}
}
