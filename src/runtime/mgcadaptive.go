// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Optimizing Go GC Adaptability and Resource Utilization through GC Throughput Control.

package runtime

import (
	"internal/runtime/atomic"
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
	// Default lower GC throughput for adaptive adjustment (0.7 * gcThroughputDefaultGoal)
	gcThroughputDefaultGoalAlpha = 0.7
	// Base value for GC throughput delta calculation
	gcThroughputDeltaBase = 30.0
	// Default GOGC value when GOGC is set as off or -1.
	defaultGCPercent = int32(100 * 1)
	// Lower limit for GOGC (30), maybe some works with large alive heap size.
	gcGOGCDefaultLowerLimit = int32(30)
)

// Indirectly export setGCAdaptiveFunc setGCThroughputGoalFunc so that user can try to access it by declare a linkname,
// even when using an sdk that doesn't provide this variable, it will get a nil value instead of a compile error.
//
//go:linkname setGCAdaptiveFunc
var setGCAdaptiveFunc = setGCAdaptive

//go:linkname setGCThroughputGoalFunc
var setGCThroughputGoalFunc = setGCThroughputGoal

//go:linkname gcAdaptiveSettingsFunc
var gcAdaptiveSettingsFunc = gcAdaptiveSettings

// gcAdaptiveControllerState represents the state of the GC adaptive controller.
var gcAdaptiveController gcAdaptiveControllerState

type gcAdaptiveControllerState struct {
	// Indicates whether the GC adaptive controller is enabled.
	gcAdaptiveEnabled bool

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

	// The range of upper and lower bounds for GC throughput goal.
	gcThroughputGoalRangeUpper float64
	gcThroughputGoalRangeLower float64
	// gcThroughputGoalRangeAlpha could be set by GOGCTHPRATIOALPHA,
	// it's used to calculate the lower bound from the upper bound for GC throughput goal.
	gcThroughputGoalRangeAlpha float64
	// Expected proportion of CPU resources allocated for actual application processing
	// relative to the total CPU resources over a period. Default is 95.
	gcThroughputGoal float64
	// Latest GC throughput goal, it may be updated in runtime.
	latestGcThroughputGoal float64
	upperBoundGoal         float64
	lowerBoundGoal         float64
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

// GOGCTHPRATIOALPHA defines the lower bound ratio for adaptive GC throughput goal range.
// It is expressed as a percentage (0-100).
func readGOGCTHPRATIOALPHA() float64 {
	p := gogetenv("GOGCTHPRATIOALPHA")
	if n, err := strconv.ParseInt(p, 10, 32); err == nil && isValidThpRatio(int32(n)) {
		// Convert percentage to a decimal fraction.
		return float64(n) / 100.0
	}
	return gcThroughputDefaultGoalAlpha
}

func isValidThpRatio[T int32 | float64](thp T) bool {
	return thp > 0 && thp <= 100
}

// int64(max) > int64(min)
func isValidThpRatioRange(max, min float64) bool {
	if isValidThpRatio(min) && isValidThpRatio(max) && int64(max) > int64(min) {
		return true
	}
	return false
}

// init initializes the state of the GC adaptive controller with the given parameters.
// It sets up initial GC percent, throughput goal, memory limit, and calculates
// the upper and lower bounds of the throughput ratio based on the given parameters.
func (c *gcAdaptiveControllerState) init(gcPercent int32, gcUpperLimit int32, memoryLimit int64,
	gcThroughputRatioGoal int32, gcThroughputRatioAlpha float64) {
	// Store the initial value of GOGC and memory limit
	// Set the initial GC throughput ratio goal
	c.initialGCPercent,
		c.initialMemoryLimit,
		c.gcThroughputGoal,
		c.gcThroughputGoalRangeUpper,
		c.gcThroughputGoalRangeLower,
		c.gcThroughputGoalRangeAlpha =
		gcPercent,
		memoryLimit,
		float64(gcThroughputRatioGoal),
		float64(gcThroughputRatioGoal),
		float64(gcThroughputRatioGoal)*gcThroughputRatioAlpha,
		gcThroughputRatioAlpha

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
	// Apply the initial GC adaptive settings.
	c.gcAdaptiveSwitch()
}

// Update GC Throughput and Bound when gcThroughputGoal is changed.
func (c *gcAdaptiveControllerState) gcThroughputGoalUpdate() {
	// First compute the new gcThroughputGoal based on gc cpu fraction.
	// If gcThroughputGoalRangeUpper == gcThroughputGoalRangeLower, it means a fixed gcThroughputGoal is set,
	// so skip the compute step.
	if isValidThpRatioRange(c.gcThroughputGoalRangeUpper, c.gcThroughputGoalRangeLower) {
		c.latestGcThroughputGoal = computeGCThroughputGoal(cpuStatusExtend.gcCpuFractionShortTermEMA.Load(),
			c.gcThroughputGoalRangeUpper,
			c.gcThroughputGoalRangeLower)
	}
	// Then update gcThroughputGoal and its bounds if it has changed.
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

// adaptGCThroughputPolicy calculates the current GC cycle's throughput and adjusts GOGC.
// It uses CPU statistics to determine the GC time, user time, and other related times,
// and then computes the GC throughput ratio. If the GC is stable, it adjusts the GOGC
// value based on the current throughput.
func (c *gcAdaptiveControllerState) adaptGCThroughputPolicy() {
	// if runtime.GC() happen, skip the gc throughput adapt for this cycle.
	if work.userForced {
		return
	}
	// Ensure that the world is stopped before calculating the CPU time stats.
	assertWorldStopped()
	// If GC is stable and user hasn't forced the update, adjust GOGC.
	// adaptGOGCPolicy dynamically adjusts the GOGC value based on the GC throughput policy.
	// If the throughput exceeds the upper bound, GOGC is decreased; if it is below the lower bound, GOGC is increased.
	// This function uses a predefined policy for adjusting GOGC based on throughput ratios.
	if gcStateInstance.checkGCStability() {
		// Update GC throughput goal and bounds.
		c.gcThroughputGoalUpdate()
		// Initial GC percent from the gcController
		currentGCPercent := gcController.gcPercent.Load()
		gcPercent := currentGCPercent
		delta, step := 0.0, 0.0
		isWithinTolerance := false

		if work.forceGcPeriod {
			// If gcforceperiod is triggered, reduce GOGC more aggressively.
			// During gcforceperiod, the gcThroughputRatio can be extremely low.
			// However, it should not be compared against the GC throughput goal,
			// since there is essentially no application work being done in this
			// phase. As a result, GC throughput is not a meaningful signal here.
			step = maxGOGCAdjustmentStep
			gcPercent = c.boundGCPercent(int32((1.0 - step) * float64(currentGCPercent)))
		} else {
			// If GC throughput exceeds the upper bound, decrease GOGC.
			if cpuStatusExtend.gcThroughputRatioEMA.Load() > c.upperBoundGoal {
				delta = cpuStatusExtend.gcThroughputRatioEMA.Load() - c.upperBoundGoal
				step = calculateAdjustmentStep(delta, gcThroughputDeltaBase)
				gcPercent = c.boundGCPercent(int32((1.0 - step) * float64(currentGCPercent)))
			} else if cpuStatusExtend.gcThroughputRatioEMA.Load() < c.lowerBoundGoal {
				delta = c.lowerBoundGoal - cpuStatusExtend.gcThroughputRatioEMA.Load()
				step = calculateAdjustmentStep(delta, gcThroughputDeltaBase)
				gcPercent = c.boundGCPercent(int32((1.0 + step) * float64(currentGCPercent)))
			} else {
				// do nothing case.
				isWithinTolerance = true
			}
		}

		if gcPercent != currentGCPercent {
			gcController.setGCPercent(gcPercent)
		}

		// Debugging output.
		if debug.gcadaptivetrace == 1 && !isWithinTolerance {
			printlock()
			print("adaptive gc pacer: gc throughput=", cpuStatusExtend.gcThroughputRatio.Load(), "->", cpuStatusExtend.gcThroughputRatioEMA.Load(), "->", c.gcThroughputGoal)
			print(" delta/step=", delta, "/", step)
			print(" gc percent=", currentGCPercent, "->", gcPercent)
			println()
			printunlock()
		}
	}
}

// calculateAdjustmentStep calculates the adjustment step for GOGC based on the delta value and bound.
func calculateAdjustmentStep(delta float64, base float64) float64 {
	step := minGOGCAdjustmentStep + delta*(maxGOGCAdjustmentStep-minGOGCAdjustmentStep)/base
	return max(min(step, maxGOGCAdjustmentStep), minGOGCAdjustmentStep)
}

// gcAdaptiveSwitch enables or disables the GC adaptive controller based on the debug settings.
func (c *gcAdaptiveControllerState) gcAdaptiveSwitch() {
	// Ensure that the world is stopped when switching GC adaptive settings.
	assertWorldStopped()
	if debug.gcadaptive == 1 {
		if !c.gcAdaptiveEnabled {
			in := c.initialGCPercent
			if in < 0 {
				// If GOGC is disabled, set it to the default GOGC value but within GOGC range.
				in = c.boundGCPercent(defaultGCPercent)
			}
			c.gcGOGCUpdate(in)
			c.gcAdaptiveEnabled = true
		}
	} else {
		if c.gcAdaptiveEnabled {
			// Restore default GOGC value.
			c.gcGOGCUpdate(c.initialGCPercent)
			c.gcAdaptiveEnabled = false
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

// setGCThroughputGoal sets GC throughput goal with default gcThroughputDefaultGoalAlpha.
func setGCThroughputGoal(thp float64) {
	if isValidThpRatio(thp) {
		gcAdaptiveController.latestGcThroughputGoal = thp
		gcAdaptiveController.gcThroughputGoalRangeUpper = thp
		gcAdaptiveController.gcThroughputGoalRangeLower = thp * gcAdaptiveController.gcThroughputGoalRangeAlpha
	}
}

// setGCThroughputGoalRange sets the upper and lower bounds alpha for the GC throughput goal range.
// alpha is a ratio (0 < alpha <= 1) to calculate the lower bound from the upper bound.
func setGCThroughputGoalRange(upper float64, alpha float64) {
	if abs(alpha-1.0) <= 1e-4 && isValidThpRatio(upper) {
		// when alpha is 1.0, it means a fixed gc throughput goal is set.
		gcAdaptiveController.gcThroughputGoalRangeUpper = upper
		gcAdaptiveController.gcThroughputGoalRangeLower = upper
		gcAdaptiveController.gcThroughputGoalRangeAlpha = alpha
	} else if alpha > 0.0 && alpha < 1.0 && isValidThpRatioRange(upper, upper*alpha) {
		gcAdaptiveController.gcThroughputGoalRangeUpper = upper
		gcAdaptiveController.gcThroughputGoalRangeLower = upper * alpha
		gcAdaptiveController.gcThroughputGoalRangeAlpha = alpha
	}
}

//go:linkname isGCAdaptiveEnabled
func isGCAdaptiveEnabled() bool {
	return debug.gcadaptive == 1
}

// -------------------------------------------------------------------------------------------------
const (
	// Number of GC cycles after which GC is considered stable
	gcStableCycleThreshold = 30
	// Duration (in seconds) after which GC is considered stable
	gcStableTimeThreshold = 30 * 1e9
	// Number of recent GC cycles to cache for heap alive size
	gcHeapAliveCacheSize = 10
	// Standard deviation ratio threshold square for heap alive size
	gcHeapAliveStdDevThresholdSquared = 0.15 * 0.15
)

type gcState struct {
	// Indicates whether GC has reached a steady state, enabling GC throughput control.
	gcStable bool
	// Cache of recent heap alive sizes.
	heapAliveCache [gcHeapAliveCacheSize]uint64
	// Timestamp of application startup.
	startupTimestamp int64
}

var gcStateInstance gcState

// checkGCStability checks if the GC has reached a stable state based on multiple conditions:
// 1. GC enters a steady state after a certain number of GC cycles (e.g., 30 cycles).
// 2. GC enters a steady state if the GC has been running for a sufficient amount of time (e.g., 30 seconds).
// 3. GC enters a steady state based on certain GC-related metrics, such as heap alive size stability.
// 4. GC enters a steady state if the heap alive size is monotonically increasing or decreasing.
func (c *gcState) checkGCStability() bool {
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
func (c *gcState) heapAliveStdDevRatioSquared() float64 {
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
func (c *gcState) heapAliveCacheValidCount() (count int32) {
	for _, v := range c.heapAliveCache {
		if v > 0 {
			count++
		}
	}
	return
}

// -------------------------------------------------------------------------------------------------
/*
```python
#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import argparse
import numpy as np
import plotly.graph_objects as go


def sigmoid_mapped(x, k, target, min_val, max_val):
    # Standard sigmoid function mapped to [min_val, max_val]
    raw = 1 / (1 + np.exp(-k * (x - target)))
    return min_val + 2 * (raw - 0.5) * (max_val - min_val)


def exp_approx_vec(x):
    # Polynomial-based exp(x) approximation for vector inputs
    LN2 = 0.6931471805599453
    MIN_EXP = 1e-14

    x = np.array(x, dtype=np.float64)

    # Range reduction: x = k * ln(2) + r
    k = np.floor(-x / LN2).astype(int)
    r = x + k * LN2

    # 5th-order Taylor expansion for exp(r)
    y = 1 + r * (1 + r * (0.5 + r * (1.0 / 6.0 + r * (1.0 / 24.0 + r * (1.0 / 120.0)))))

    # Reconstruct exp(x) = exp(r) * 2^(-k)
    y = y * np.power(0.5, k)
    y = np.maximum(y, MIN_EXP)
    return y


def approximate_sigmoid_mapped(x, k, target, min_val, max_val):
    # Sigmoid mapping using exp approximation
    raw = 1 / (1 + exp_approx_vec(-k * (x - target)))
    return min_val + 2 * (raw - 0.5) * (max_val - min_val)


def calc_app_cpu_rate(gc_cpu_rate, gc_throughput):
    # Compute application CPU utilization based on GC CPU rate and GC throughput
    return (gc_cpu_rate * gc_throughput) / (1 - gc_throughput)


def theory_max_throughput(gc_cpu_rate):
    # Theoretical upper bound of GC throughput
    return 1 - gc_cpu_rate


def main():
    # -------------------------
    # Argument parsing
    # -------------------------
    parser = argparse.ArgumentParser(description="GC Throughput Visualization")
    parser.add_argument("--k", type=float, default=4.0)
    parser.add_argument("--target", type=float, default=0.0)
    parser.add_argument("--min", dest="min_val", type=float, default=0.5)
    parser.add_argument("--max", dest="max_val", type=float, default=0.95)
    parser.add_argument("--x-max", type=float, default=10.0)
    args = parser.parse_args()

    if args.max_val <= args.min_val:
        raise ValueError("max must be greater than min")

    # X-axis: GC CPU overhead rate (percentage)
    gc_cpu_rate = np.linspace(0, args.x_max, 1000)

    # GC throughput modeled by sigmoid mapping
    gc_throughput = sigmoid_mapped(
        gc_cpu_rate, args.k, args.target, args.min_val, args.max_val
    )

    # Theoretical throughput upper bound
    max_throughput = theory_max_throughput(gc_cpu_rate / 100)

    # Application CPU utilization
    app_cpu_rate = calc_app_cpu_rate(
        gc_cpu_rate / 100,
        np.minimum(gc_throughput, max_throughput),
    )

    # GC-to-App CPU utilization ratio
    gc_app_ratio = (gc_cpu_rate / 100) / np.maximum(app_cpu_rate, 1e-6)

    fig = go.Figure()

    # =========================
    # Primary curves
    # =========================
    fig.add_trace(
        go.Scatter(
            x=gc_cpu_rate,
            y=gc_throughput,
            mode="lines",
            name="GC Throughput (GC / (GC + App))",
            line=dict(width=3),
            hovertemplate="GC Throughput: %{y:.2%}<extra></extra>",
        )
    )

    fig.add_trace(
        go.Scatter(
            x=gc_cpu_rate,
            y=app_cpu_rate,
            mode="lines",
            name="App CPU Utilization (App / Total)",
            line=dict(width=2, dash="dot"),
            hovertemplate="App CPU Utilization: %{y:.2%}<extra></extra>",
        )
    )

    # =========================
    # GC-to-App ratio (right Y-axis)
    # =========================
    fig.add_trace(
        go.Scatter(
            x=gc_cpu_rate,
            y=gc_app_ratio,
            mode="lines",
            name="GC/App CPU Ratio",
            line=dict(width=2, dash="dash"),
            yaxis="y2",
            hovertemplate="GC/App Ratio: %{y:.2%}<extra></extra>",
        )
    )

    # =========================
    # Theoretical upper bound
    # =========================
    fig.add_trace(
        go.Scatter(
            x=gc_cpu_rate,
            y=max_throughput,
            mode="lines",
            name="Theoretical Upper Bound",
            line=dict(width=1.5, dash="dot"),
            opacity=0.5,
            hovertemplate="Theoretical Upper Bound: %{y:.2%}<extra></extra>",
        )
    )

    # =========================
    # Reference lines (left axis)
    # =========================
    for y in [0.5, 0.75, 0.9]:
        fig.add_hline(
            y=y,
            line=dict(color="gray", width=1, dash="dot"),
            opacity=0.4,
        )

    # =========================
    # Reference lines (right axis: GC/App ratio)
    # =========================
    for y in [0.1, 0.25, 0.5, 1.0]:
        fig.add_hline(
            y=y,
            yref="y2",
            line=dict(color="red", width=1, dash="dot"),
            opacity=0.25,
        )

    # =========================
    # Recommended operating region
    # =========================
    fig.add_vrect(
        x0=0,
        x1=5,
        fillcolor="LightGreen",
        opacity=0.08,
        layer="below",
        line_width=0,
    )

    fig.update_layout(
        title=f"GC Throughput Simulation (Sigmoid K = {args.k})",
        xaxis_title="GC CPU Overhead Rate (%)",
        yaxis_title="Rate",
        yaxis=dict(
            tickformat=".0%",
            dtick=0.05,
            showgrid=True,
            gridwidth=1,
            griddash="dot",
        ),
        yaxis2=dict(
            title="GC/App Ratio",
            overlaying="y",
            side="right",
            showgrid=False,
        ),
        xaxis=dict(
            ticksuffix="%",
            dtick=1,
            showgrid=True,
            gridwidth=1,
            griddash="dot",
        ),
        hovermode="x unified",
        template="plotly_white",
    )

    fig.show()


if __name__ == "__main__":
    main()
*/
const (
	// Defalut K value for sigmoid function in gc throughput goal adjustment
	sigmoidKDefaultValue = 3.0
)

// GOGCTHPSIGMOIDK defines the K value for the sigmoid function used in GC throughput goal adjustment.
func readSIGMOIDKVALUE() float64 {
	p := gogetenv("GOGCTHPSIGMOIDK")
	if f, err := strconv.ParseInt(p, 10, 32); err == nil && f > 0 {
		return float64(f)
	}
	return sigmoidKDefaultValue
}

func setSIGMOIDKVALUE(k float64) {
	if k > 0.0 {
		sigmoidKValue = k
	}
}

// sigmoid K value for gc throughput goal adjustment.
var sigmoidKValue float64 = sigmoidKDefaultValue

// assume target is 0, in the case y = 0.50 when x = 0.
// we will linearly map x from sigmod function output range (0.50,1.00) to (min, max).
func sigmoidLinearMapping(x float64, k float64, max float64, min float64) float64 {
	y := sigmoidApprox(x, k, 0.0)
	// because target is always 0, so y is 0.50 when x is 0.
	return min + 2*(y-0.50)*(max-min)
}

// computeGCThroughputGoal computes the GC throughput goal based on the GC CPU fraction.
// It uses a sigmoid-based linear mapping to determine the throughput goal
// according to the gcCpuFraction value.
func computeGCThroughputGoal(gcCpuFraction float64, upper float64, lower float64) float64 {
	return sigmoidLinearMapping(
		gcCpuFraction,
		sigmoidKValue,
		upper/100,
		lower/100) * 100.0
}

// -------------------------------------------------------------------------------------------------
// cpuStatusExtend holds extended CPU statistics for GC throughput calculations.
// include gc overhead and gc/(gc+app) cpu fraction.
const (
	// Alpha value for exponential moving average (EMA) in throughput calculation
	gcThroughputEMAAlpha = 0.4
	// IdleMarkTime impact factor
	gcIdleMarkTimeFactor = 2
	// Make sure GC throughput is computed at least every gcThroughputComputeIntervalNs to capture the latest GC performance.
	gcThroughputComputeIntervalNs = 40 * 1e9 // 40 seconds
)

var cpuStatusExtend cpuStatsExtend

type cpuStatsExtend struct {
	// Indicates whether the accumulated time has been initialized.
	accumTimeInitialized bool
	// Actual CPU resource ratio used by the application compared to process CPU resources.
	gcThroughputRatio atomic.Float64
	// Exponential moving average (EMA) ratio of GC CPU time to process CPU time.
	gcThroughputRatioEMA   atomic.Float64
	gcCpuFractionShortTerm atomic.Float64
	// Exponential moving average (EMA) ratio of GC CPU time to total CPU time.
	gcCpuFractionShortTermEMA atomic.Float64
	// Accumulated time spent until the previous GC cycle.
	prevAccumGCTime       int64
	prevAccumUserTime     int64
	prevAccumScavTime     int64
	prevAccumTotalTime    int64
	precomputeTimestampNs int64 // Timestamp for pre GC throughput computation time.
}

// emaSmoothing applies an exponential moving average (EMA) update.
// first parameter is the high value, second parameter is the light value.
// Returns:
//   - The updated EMA value.
func emaSmoothing(highValue, lightValue float64) float64 {
	if gcThroughputEMAAlpha < 0.5 {
		return lightValue*gcThroughputEMAAlpha + (1.0-gcThroughputEMAAlpha)*highValue
	} else {
		return highValue*gcThroughputEMAAlpha + (1.0-gcThroughputEMAAlpha)*lightValue
	}
}

func gcUserTimePercent(userTime, totalTime int64) float64 {
	return max(min(float64(userTime)*100.0/float64(totalTime), 100.0), 0.0)
}

// computeGCThroughput calculates the Garbage Collection (GC) throughput as a percentage.
//
// GC throughput is defined as the proportion of time spent executing user code
// relative to the total time, including user execution time, GC time, and memory scavenging time.
//
// Formula:
//
//	GC Throughput = (User Time / (User Time + GC Time + Scavenge Time + Alpha * Idle Mark Time)) * 100
//	GC CPU Fraction (Short Term) = ((GC Time + Idle Mark Time) / Total Time) * 100
//
// Where:
// - userTime: Time spent executing user code.
// - gcTime: Time spent running the garbage collector.
// - scavengeTime: Time spent in memory scavenging.
// - totalTime: Total elapsed time.
// - The result is capped at 100.0% to prevent any overflow due to precision errors.
func (c *cpuStatsExtend) computeGCThroughput() {
	// if runtime.GC() happen, skip the gc throughput calculation for this cycle.
	// If user always forces GC and never lets GC run in its own pace, make sure gc throughput is
	// computed at least every gcThroughputComputeIntervalNs to capture the latest GC performance.
	if work.userForced && nanotime()-c.precomputeTimestampNs < gcThroughputComputeIntervalNs {
		return
	}
	if c.accumTimeInitialized {
		// Exclude part of idle CPU time from the GC time calculation.
		// To balance the relationship between actual GC throughput and business CPU utilization,
		// the factor of GC idle mark time is also taken into account.
		gcIdleTime := gcController.idleMarkTime.Load()
		gcTime := work.cpuStats.GCTotalTime - c.prevAccumGCTime - gcIdleTime
		userTime := work.cpuStats.UserTime - c.prevAccumUserTime
		scavengeTime := work.cpuStats.ScavengeTotalTime - c.prevAccumScavTime
		totalTime := work.cpuStats.TotalTime - c.prevAccumTotalTime
		c.gcThroughputRatio.Store(gcUserTimePercent(userTime, gcTime+userTime+scavengeTime+gcIdleTime/gcIdleMarkTimeFactor))
		c.gcThroughputRatioEMA.Store(emaSmoothing(c.gcThroughputRatio.Load(), c.gcThroughputRatioEMA.Load()))
		// Here we will take gcIdleTime into account to calculate short-term GC CPU fraction.
		c.gcCpuFractionShortTerm.Store(gcUserTimePercent(gcTime+gcIdleTime+scavengeTime, totalTime))
		// Here we use memstats.gc_cpu_fraction to make gcCpuFractionShortTermEMA be more smooth.
		c.gcCpuFractionShortTermEMA.Store(emaSmoothing(c.gcCpuFractionShortTerm.Load(), memstats.gc_cpu_fraction*100.0))
	}
	c.prevAccumGCTime = work.cpuStats.GCTotalTime
	c.prevAccumUserTime = work.cpuStats.UserTime
	c.prevAccumScavTime = work.cpuStats.ScavengeTotalTime
	c.prevAccumTotalTime = work.cpuStats.TotalTime
	c.accumTimeInitialized = true
	c.precomputeTimestampNs = nanotime()
}

// -------------------------------------------------------------------------------------------------
var gcadaptiveFuncMap = map[string]any{
	"setGCAdaptive":            setGCAdaptive,
	"setGCThroughputGoal":      setGCThroughputGoal,
	"setGCThroughputGoalRange": setGCThroughputGoalRange,
	"setSIGMOIDKVALUE":         setSIGMOIDKVALUE,
}

func gcAdaptiveSettings(funcName string, params ...any) {
	switch funcName {
	case "setGCAdaptive":
		if !(len(params) == 1 && params[0] != nil) {
			return
		}
		if enable, ok := params[0].(bool); ok {
			setGCAdaptive(enable)
		}
	case "setGCThroughputGoal":
		if !(len(params) == 1 && params[0] != nil) {
			return
		}
		if thp, ok := params[0].(float64); ok {
			setGCThroughputGoal(thp)
		}
	case "setGCThroughputGoalRange":
		if !(len(params) == 2 && params[0] != nil && params[1] != nil) {
			return
		}
		upper, ok1 := params[0].(float64)
		alpha, ok2 := params[1].(float64)
		if ok1 && ok2 {
			setGCThroughputGoalRange(upper, alpha)
		}
	case "setSIGMOIDKVALUE":
		if !(len(params) == 1 && params[0] != nil) {
			return
		}
		if kValue, ok := params[0].(float64); ok {
			setSIGMOIDKVALUE(kValue)
		}
	}
}

// -------------------------------------------------------------------------------------------------
func init() {
	sigmoidKValue = readSIGMOIDKVALUE()
	gcStateInstance.startupTimestamp = nanotime()
	cpuStatusExtend.precomputeTimestampNs = nanotime()
}
