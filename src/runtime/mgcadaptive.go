// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Optimizing Go GC Adaptability and Resource Utilization through GC Throughput Control.

package runtime

import (
	"internal/runtime/math"
	"internal/strconv"
	_ "unsafe"
)

const (
	// Allowable error margin for GC throughput, within -0.75% ~ +0.75%
	gcThroughputTolerance = 0.0075
	// Minimum step size for GOGC adjustment (10%)
	minGOGCAdjustmentStep = 0.1
	// Maximum step size for GOGC adjustment (40%)
	maxGOGCAdjustmentStep = 0.4
	// Default goal for GC throughput (95%)
	gcThroughputDefaultGoal = 95.0
	// Base value for GC throughput delta calculation
	gcThroughputDeltaBase = 30.0
	// Default GOGC value when GOGC is set as off or -1.
	defaultGCPercent = int32(100 * 1)
	// Lower limit for GOGC (50)
	gcGOGCDefaultLowerLimit = int32(50)
	// Alpha value for exponential moving average (EMA) in throughput calculation
	gcThroughputEMAAlpha = 0.4
	// IdleMarkTime impact factor
	gcIdleMarkTimeFactor = 2
	// Number of GC cycles after which GC is considered stable
	gcStableCycleThreshold = 30
	// Duration (in seconds) after which GC is considered stable
	gcStableTimeThreshold = 30 * 1e9
	// Number of recent GC cycles to cache for heap alive size
	gcHeapAliveCacheSize = 10
	// Standard deviation ratio threshold square for heap alive size
	gcHeapAliveStdDevThresholdSquared = 0.15 * 0.15
)

// Indirectly export setGCAdaptiveFunc setGCThroughputGoalFunc so that user can try to access it by declare a linkname,
// even when using an sdk that doesn't provide this variable, it will get a nil value instead of a compile error.
//
//go:linkname setGCAdaptiveFunc
var setGCAdaptiveFunc = setGCAdaptive

//go:linkname setGCThroughputGoalFunc
var setGCThroughputGoalFunc = setGCThroughputGoal

// gcAdaptiveControllerState represents the state of the GC adaptive controller.
var gcAdaptiveController gcAdaptiveControllerState

type gcAdaptiveControllerState struct {
	// Indicates whether the GC adaptive controller is enabled.
	gcAdaptiveEnabled bool
	// Indicates whether GC has reached a steady state, enabling GC throughput control.
	gcStable bool

	// Backup of initial GOGC at startup.
	initialGCPercent int32
	// Backup of GOMEMLIMIT at startup.
	initialMemoryLimit int64

	// gcGOGCUpperLimit defines the upper limit for GOGC.
	// gcGOGCUpperLimit is set to the env GOGC at startup.
	gcGOGCUpperLimit int32
	// gcGOGCLowerLimit defines the lower limit for GOGC.
	// gcGOGCLowerLimit is set as gcGOGCDefaultLowerLimit if it's < env GOGC.
	// gcGOGCLowerLimit is set as env GOGC if gcGOGCDefaultLowerLimit > env GOGC.
	gcGOGCLowerLimit int32

	// Expected proportion of CPU resources allocated for actual application processing
	// relative to the total CPU resources over a period. Default is 95.
	gcThroughputGoal float64
	// Latest GC throughput goal, it may be updated in runtime.
	latestGcThroughputGoal float64
	upperBoundGoal         float64
	lowerBoundGoal         float64
	// Actual CPU resource ratio used by the application compared to total CPU resources.
	gcThroughputRatio float64
	// Exponential moving average (EMA) ratio of GC CPU time to total CPU time.
	gcThroughputEMA float64

	// Cache of recent heap alive sizes.
	heapAliveCache [gcHeapAliveCacheSize]uint64

	// Timestamp of application startup.
	startupTimestamp int64
}

func isValidThpRatio[T int32 | float64](thp T) bool {
	return thp > 0 && thp <= 100
}

func readGOGCUPPERLIMIT() int32 {
	p := gogetenv("GOGCUPPERLIMIT")
	if n, err := strconv.ParseInt(p, 10, 32); err == nil && n > 0 {
		return int32(n)
	}
	return -1
}

func readGOGCTHPRATIO() int32 {
	p := gogetenv("GOGCTHPRATIO")
	if n, err := strconv.ParseInt(p, 10, 32); err == nil && isValidThpRatio(int32(n)) {
		return int32(n)
	}
	return gcThroughputDefaultGoal
}

// emaSmoothing applies an exponential moving average (EMA) update.
//
// Returns:
//   - The updated EMA value.
func emaSmoothing(newValue, oldValue float64) float64 {
	return gcThroughputEMAAlpha*newValue + (1.0-gcThroughputEMAAlpha)*oldValue
}

// init initializes the state of the GC adaptive controller with the given parameters.
// It sets up initial GC percent, throughput goal, memory limit, and calculates
// the upper and lower bounds of the throughput ratio based on the given parameters.
func (c *gcAdaptiveControllerState) init(gcPercent int32, gcUpperLimit int32, gcThroughputRatioGoal int32, memoryLimit int64) {
	// Initialize the state flags.
	c.gcStable = false

	// Store the initial value of GOGC and memory limit
	// Set the initial GC throughput ratio goal
	c.initialGCPercent, c.initialMemoryLimit, c.gcThroughputGoal = gcPercent, memoryLimit, float64(gcThroughputRatioGoal)

	// Calculate the upper and lower bounds for the throughput ratio goal
	// The upper bound is the goal plus the error margin, capped at 100%.
	// The lower bound is the goal minus the error margin, capped at 10%.
	c.upperBoundGoal = min(c.gcThroughputGoal*(1.0+gcThroughputTolerance), 100.0)
	c.lowerBoundGoal = max(c.gcThroughputGoal*(1.0-gcThroughputTolerance), 10.0)
	c.latestGcThroughputGoal = c.gcThroughputGoal

	// Store the upper limit for GOGC
	if gcPercent < 0 {
		// If GC is disabled but we're running a forced GC,
		// act like GOGC is huge for the below calculations.
		c.gcGOGCUpperLimit = 100000
		c.gcGOGCLowerLimit = gcGOGCDefaultLowerLimit
	} else {
		c.gcGOGCUpperLimit = gcPercent
		c.gcGOGCLowerLimit = min(gcPercent, gcGOGCDefaultLowerLimit)
	}
	// If GOGCUPPERLIMIT is set, update the upper limit for GOGC.
	if gcUpperLimit > 0 {
		c.gcGOGCUpperLimit = gcUpperLimit
		c.gcGOGCLowerLimit = min(c.gcGOGCUpperLimit, c.gcGOGCLowerLimit)
	}

	// Store the application startup timestamp
	c.startupTimestamp = nanotime()

	gcAdaptiveSwitch()
}

// Update GC Throughput and Bound when gcThroughputGoal is changed.
func (c *gcAdaptiveControllerState) gcThroughputUpdate() {
	if c.latestGcThroughputGoal != c.gcThroughputGoal && isValidThpRatio(c.latestGcThroughputGoal) {
		c.gcThroughputGoal = c.latestGcThroughputGoal
		c.upperBoundGoal = min(c.gcThroughputGoal*(1.0+gcThroughputTolerance), 100.0)
		c.lowerBoundGoal = max(c.gcThroughputGoal*(1.0-gcThroughputTolerance), 10.0)
	}
}

// Update GOGC and set it.
func (c *gcAdaptiveControllerState) gcGOGCUpdate(in int32) {
	if gcController.gcPercent.Load() != in {
		gcController.setGCPercent(in)
		systemstack(gcControllerCommit)
	}
}

// estimateMaxGOGC estimates the upper bound for GOGC based on memory limits.
//
// If GOMEMLIMIT is set, this function calculates a reasonable GOGC limit
// using the difference between the memory limit heap goal and the current
// live heap size. If no memory limit is set, it returns the predefined upper
// threshold (`gcGOGCUpperLimit`).
//
// This function ensures that division by zero is avoided when computing the
// ratio of heap growth.
//
// Returns:
//   - An estimated GOGC value within a valid range.
func (c *gcAdaptiveControllerState) estimateMaxGOGC() int32 {
	if gcController.memoryLimit.Load() != math.MaxInt64 {
		heapGoal := gcController.memoryLimitHeapGoal()
		heapMarkedLive := gcController.heapLive.Load()
		if heapMarkedLive > 0 {
			if heapGoal > heapMarkedLive {
				return max(int32(float64(heapGoal-heapMarkedLive)*100.0/float64(heapMarkedLive)), c.gcGOGCLowerLimit)
			} else {
				return c.gcGOGCLowerLimit
			}
		}
	}
	return c.gcGOGCUpperLimit
}

// boundGCPercent ensures that the given gcPercent value is within a valid range.
// It limits gcPercent to be between the estimated maximum GOGC value and the lower GOGC limit.
func (c *gcAdaptiveControllerState) boundGCPercent(gcPercent int32) int32 {
	return max(min(gcPercent, c.estimateMaxGOGC()), c.gcGOGCLowerLimit)
}

func computeGCThroughput(userTime, totalTime int64) float64 {
	return max(min(float64(userTime)*100.0/float64(totalTime), 100.0), 0.0)
}

// adaptGCThroughput calculates the current GC cycle's throughput and adjusts GOGC.
// It uses CPU statistics to determine the GC time, user time, and other related times,
// and then computes the GC throughput ratio. If the GC is stable, it adjusts the GOGC
// value based on the current throughput.
func (c *gcAdaptiveControllerState) adaptGCThroughput() {
	// Ensure that the world is stopped before calculating the CPU time stats.
	assertWorldStopped()
	// Calculate the GC throughput ratio, ensuring it doesn't exceed 100%.
	// User time includes both GC and scavenge time.
	c.gcThroughputRatio = cpuStatusExtend.gcThroughput.Load()
	c.gcThroughputEMA = emaSmoothing(c.gcThroughputRatio, c.gcThroughputEMA)

	// If GC is stable and user hasn't forced the update, adjust GOGC.
	if c.checkGCStability() && !work.userForced {
		c.adaptGOGCPolicy()
	}
}

// calculateAdjustmentStep calculates the adjustment step for GOGC based on the delta value and bound.
func calculateAdjustmentStep(delta float64, base float64) float64 {
	step := minGOGCAdjustmentStep + delta*(maxGOGCAdjustmentStep-minGOGCAdjustmentStep)/base
	return max(min(step, maxGOGCAdjustmentStep), minGOGCAdjustmentStep)
}

// adaptGOGCPolicy dynamically adjusts the GOGC value based on the GC throughput policy.
// If the throughput exceeds the upper bound, GOGC is decreased; if it is below the lower bound, GOGC is increased.
// This function uses a predefined policy for adjusting GOGC based on throughput ratios.
func (c *gcAdaptiveControllerState) adaptGOGCPolicy() {
	// Initial GC percent from the gcController
	currentGCPercent := gcController.gcPercent.Load()
	gcPercent := currentGCPercent
	delta, step := 0.0, 0.0
	isWithinTolerance := false

	// If GC throughput exceeds the upper bound, decrease GOGC.
	if c.gcThroughputEMA > c.upperBoundGoal {
		delta = c.gcThroughputEMA - c.upperBoundGoal
		step = calculateAdjustmentStep(delta, gcThroughputDeltaBase)
		gcPercent = c.boundGCPercent(int32((1.0 - step) * float64(currentGCPercent)))
	} else if c.gcThroughputEMA < c.lowerBoundGoal {
		delta = c.lowerBoundGoal - c.gcThroughputEMA
		step = calculateAdjustmentStep(delta, gcThroughputDeltaBase)
		gcPercent = c.boundGCPercent(int32((1.0 + step) * float64(currentGCPercent)))
	} else {
		// do nothing case.
		isWithinTolerance = true
	}

	if gcPercent != currentGCPercent {
		gcController.setGCPercent(gcPercent)
	}

	// Debugging output.
	if debug.gcadaptivetrace == 1 && !isWithinTolerance {
		printlock()
		print("adaptive gc pacer: gc throughput=", c.gcThroughputRatio, "->", c.gcThroughputEMA, "->", c.gcThroughputGoal)
		print(" delta/step=", delta, "/", step)
		print(" gc percent=", currentGCPercent, "->", gcPercent)
		println()
		printunlock()
	}
}

// checkGCStability checks if the GC has reached a stable state based on multiple conditions:
// 1. GC enters a steady state after a certain number of GC cycles (e.g., 30 cycles).
// 2. GC enters a steady state if the GC has been running for a sufficient amount of time (e.g., 30 seconds).
// 3. GC enters a steady state based on certain GC-related metrics, such as heap alive size stability.
// 4. GC enters a steady state if the heap alive size is monotonically increasing or decreasing.
func (c *gcAdaptiveControllerState) checkGCStability() bool {
	if !c.gcStable {
		// Check if the GC has completed enough cycles or has run for enough time to be considered stable
		if memstats.numgc > gcStableCycleThreshold ||
			(c.startupTimestamp > 0 && nanotime()-c.startupTimestamp > gcStableTimeThreshold) {
			c.gcStable = true
		} else {
			// Check if the heap alive size has stabilized over recent cycles
			c.heapAliveCache[memstats.numgc%gcHeapAliveCacheSize] = gcController.heapLive.Load()
			if c.heapAliveCacheValidCount() == gcHeapAliveCacheSize && c.heapAliveStdDevRatioSquared() < gcHeapAliveStdDevThresholdSquared {
				c.gcStable = true
			}
		}
	}
	return c.gcStable
}

// heapAliveStdDevRatioSquared calculates the square of the standard deviation ratio
// for the heap alive size over the recent GC cycles.
func (c *gcAdaptiveControllerState) heapAliveStdDevRatioSquared() float64 {
	var sum uint64
	// Calculate the sum of all heap alive sizes in the cache
	for _, v := range c.heapAliveCache {
		sum += v
	}

	// Calculate the mean of heap alive sizes
	mean := float64(sum) / float64(len(c.heapAliveCache))

	var sumOfSquares float64
	// Calculate the variance
	for _, v := range c.heapAliveCache {
		sumOfSquares += (float64(v) - mean) * (float64(v) - mean)
	}

	// Return the squared standard deviation ratio: (variance / mean^2)
	return (sumOfSquares / float64(len(c.heapAliveCache))) / (mean * mean)
}

// heapAliveCacheValidCount returns the count of valid heap alive sizes in the cache.
// A valid size is one that is greater than zero.
func (c *gcAdaptiveControllerState) heapAliveCacheValidCount() (count int32) {
	for _, v := range c.heapAliveCache {
		if v > 0 {
			count++
		}
	}
	return
}

// gcAdaptiveSwitch enables or disables the GC adaptive controller based on the debug settings.
func gcAdaptiveSwitch() {
	// Ensure that the world is stopped when switching GC adaptive settings.
	assertWorldStopped()
	if debug.gcadaptive == 1 {
		if !gcAdaptiveController.gcAdaptiveEnabled {
			in := gcAdaptiveController.initialGCPercent
			if in < 0 {
				// If GOGC is disabled, set it to the default GOGC value but within GOGC range.
				in = gcAdaptiveController.boundGCPercent(defaultGCPercent)
			}
			gcAdaptiveController.gcGOGCUpdate(in)
			gcAdaptiveController.gcAdaptiveEnabled = true
		}
		gcAdaptiveController.gcThroughputUpdate()
	} else {
		if gcAdaptiveController.gcAdaptiveEnabled {
			// Restore default GOGC value.
			gcAdaptiveController.gcGOGCUpdate(gcAdaptiveController.initialGCPercent)
			gcAdaptiveController.gcAdaptiveEnabled = false
		}
	}
}

func setGCAdaptive(enable bool) {
	if enable {
		debug.gcadaptive = 1
	} else {
		debug.gcadaptive = 0
	}
	// Trigger a GC cycle to apply the new settings.
	// This is a workaround to ensure that the GC adaptive controller is re-initialized
	// and the GOGC value is set correctly.
	GC()
}

func setGCThroughputGoal(thp float64) {
	if isValidThpRatio(thp) {
		gcAdaptiveController.latestGcThroughputGoal = thp
	}
}

//go:linkname isGCAdaptiveEnabled
func isGCAdaptiveEnabled() bool {
	return debug.gcadaptive == 1
}
