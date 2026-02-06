// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"bytes"
	"math"
	"os"
	"os/exec"
	. "runtime"
	"strconv"
	"testing"
	_ "unsafe"
)

const (
	maxUint64 = ^uint64(0)
	maxInt64  = int64(maxUint64 >> 1)
)

var (
	gcAdaptiveSink interface{}
)

//go:linkname setGCAdaptiveFunc runtime.setGCAdaptiveFunc
var setGCAdaptiveFunc func(enable bool)

//go:linkname setGCThroughputGoalFunc runtime.setGCThroughputGoalFunc
var setGCThroughputGoalFunc func(throughput float64)

//go:linkname setGCAdaptiveSettingsFunc runtime.gcAdaptiveSettingsFunc
var setGCAdaptiveSettingsFunc func(funcName string, params ...any)

//go:linkname setGCAdaptiveFuncNotExist runtime.setGCAdaptiveFuncNotExist
var setGCAdaptiveFuncNotExist func(bytes int64) int64

//go:linkname setGCThroughputGoalFuncNotExist runtime.setGCThroughputGoalFuncNotExist
var setGCThroughputGoalFuncNotExist func(throughput float64)

//go:linkname setGCAdaptiveSettingsFuncNotExist runtime.setGCAdaptiveSettingsFuncNotExist
var setGCAdaptiveSettingsFuncNotExist func(funcName string, params ...any)

// Check if the function setGCAdaptiveFunc is linked correctly
func TestSymbolExportedGCAdaptive(t *testing.T) {
	if setGCAdaptiveFuncNotExist != nil {
		t.Errorf("linkname a function that does not exist return not nil")
	}

	if setGCAdaptiveFunc == nil {
		t.Errorf("linkname an existing function return nil")
	}

	if setGCThroughputGoalFuncNotExist != nil {
		t.Errorf("linkname a function that does not exist return not nil")
	}

	if setGCThroughputGoalFunc == nil {
		t.Errorf("linkname an existing function return nil")
	}

	if setGCAdaptiveSettingsFuncNotExist != nil {
		t.Errorf("linkname a function that does not exist return not nil")
	}

	if setGCAdaptiveSettingsFunc == nil {
		t.Errorf("linkname an existing function return nil")
	}
}

func TestComputeGCThroughput(t *testing.T) {
	tests := []struct {
		name      string
		userTime  int64
		totalTime int64
		want      float64
	}{
		{
			name:      "normal case",
			userTime:  30,
			totalTime: 100,
			want:      30.0,
		},
		{
			name:      "zero user time",
			userTime:  0,
			totalTime: 100,
			want:      0.0,
		},
		{
			name:      "full usage",
			userTime:  100,
			totalTime: 100,
			want:      100.0,
		},
		{
			name:      "userTime greater than totalTime (clamp to 100)",
			userTime:  150,
			totalTime: 100,
			want:      100.0,
		},
		{
			name:      "negative userTime (clamp to 0)",
			userTime:  -10,
			totalTime: 100,
			want:      0.0,
		},
		{
			name:      "large values",
			userTime:  5_000_000_000,
			totalTime: 10_000_000_000,
			want:      50.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeGCThroughput(tt.userTime, tt.totalTime)
			if got != tt.want {
				t.Fatalf(
					"ComputeGCThroughput(%d, %d) = %f, want %f",
					tt.userTime, tt.totalTime, got, tt.want,
				)
			}
		})
	}
}

func TestCalculateAdjustmentStep(t *testing.T) {
	tests := []struct {
		name     string
		delta    float64
		base     float64
		expected float64
	}{
		{
			name:     "delta is zero",
			delta:    0,
			base:     GCThroughputDeltaBase,
			expected: MinGOGCAdjustmentStep,
		},
		{
			name:     "delta equals base (max step)",
			delta:    GCThroughputDeltaBase,
			base:     GCThroughputDeltaBase,
			expected: MaxGOGCAdjustmentStep,
		},
		{
			name:     "delta is half of base (mid step)",
			delta:    GCThroughputDeltaBase / 2,
			base:     GCThroughputDeltaBase,
			expected: (MinGOGCAdjustmentStep + MaxGOGCAdjustmentStep) / 2,
		},
		{
			name:     "delta above base (clamped to max)",
			delta:    GCThroughputDeltaBase * 2,
			base:     GCThroughputDeltaBase,
			expected: MaxGOGCAdjustmentStep,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := ComputeAdjustmentStep(tt.delta, tt.base)
			if math.Abs(actual-tt.expected) > 1e-6 {
				t.Errorf("test name %s: expected %.6f, got %.6f", tt.name, tt.expected, actual)
			}
		})
	}
}

func TestEmaSmoothing(t *testing.T) {
	tests := []struct {
		name      string
		high, low float64
		expected  float64
	}{
		{
			name:     "simple_weighted_average",
			high:     100.0,
			low:      50.0,
			expected: 80.0,
		},
		{
			name:     "zero_light",
			high:     100.0,
			low:      0.0,
			expected: 60.0,
		},
		{
			name:     "zero_high",
			high:     0.0,
			low:      50.0,
			expected: 20.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeEMASmoothing(tt.high, tt.low)
			if math.Abs(got-tt.expected) > 1e-3 {
				t.Fatalf("got %f, want %f", got, tt.expected)
			}
		})
	}
}

func TestComputeGCThroughputGoal(t *testing.T) {
	testUpper := 95.0
	testLower := 95.0 * 0.7
	tests := []struct {
		name      string
		gcCpuFrac float64
		upper     float64
		lower     float64
		k         float64
		expected  float64
	}{
		{
			name:      "k3_x0.1",
			gcCpuFrac: 0.1,
			upper:     testUpper,
			lower:     testLower,
			k:         3.0,
			expected:  70.74,
		},
		{
			name:      "k3_x0.5",
			gcCpuFrac: 0.5,
			upper:     testUpper,
			lower:     testLower,
			k:         3.0,
			expected:  84.60,
		},
		{
			name:      "k3_x0.8",
			gcCpuFrac: 0.8,
			upper:     testUpper,
			lower:     testLower,
			k:         3.0,
			expected:  90.25,
		},
		{
			name:      "k3_x1.5",
			gcCpuFrac: 1.5,
			upper:     testUpper,
			lower:     testLower,
			k:         3.0,
			expected:  94.37,
		},
		{
			name:      "k3_x2.0",
			gcCpuFrac: 2.0,
			upper:     testUpper,
			lower:     testLower,
			k:         3.0,
			expected:  94.85,
		},
		{
			name:      "k4_x0.1",
			gcCpuFrac: 0.1,
			upper:     testUpper,
			lower:     testLower,
			k:         4.0,
			expected:  72.12,
		},
		{
			name:      "k4_x0.5",
			gcCpuFrac: 0.5,
			upper:     testUpper,
			lower:     testLower,
			k:         4.0,
			expected:  88.20,
		},
		{
			name:      "k4_x0.8",
			gcCpuFrac: 0.8,
			upper:     testUpper,
			lower:     testLower,
			k:         4.0,
			expected:  92.76,
		},
		{
			name:      "k4_x1.5",
			gcCpuFrac: 1.5,
			upper:     testUpper,
			lower:     testLower,
			k:         4.0,
			expected:  94.85,
		},
		{
			name:      "k4_x2.0",
			gcCpuFrac: 2.0,
			upper:     testUpper,
			lower:     testLower,
			k:         4.0,
			expected:  94.98,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := ComputeGCThroughputGoal(tt.gcCpuFrac, tt.upper, tt.lower, tt.k)
			if math.Abs(actual-tt.expected) > 1e-2 {
				t.Errorf("test name %s: expected %.6f, got %.6f", tt.name, tt.expected, actual)
			}
		})
	}
}

func checkGCAdaptiveState(t *testing.T, expected bool) {
	state := IsGCAdaptiveIsEnabled()
	if state != expected {
		t.Fatalf("gc adaptive state should be %v, got %v.", expected, state)
	}
}

func checkGCPercent(t *testing.T, expected int32, gcPercent int32) {
	if gcPercent != expected {
		t.Fatalf("gc percent should be %v, got %v.", expected, gcPercent)
	}
}

func checkGCPercentInRange(t *testing.T, gcPercent int32, scope [2]int32) {
	if gcPercent < scope[0] || gcPercent > scope[1] {
		t.Fatalf("gc percent should be in range %v, got %v.", scope, gcPercent)
	}
}

func checkMemoryLimit(t *testing.T, expected int64, memoryLimit int64) {
	if memoryLimit != expected {
		t.Fatalf("initial memory limit should be %v, got %v.", expected, memoryLimit)
	}
}

func checkGCThroughputBoundRange(t *testing.T, upperBound float64, lowerBound float64, gcThroughputUpper float64, gcThroughputLower float64) {
	if math.Abs(lowerBound-gcThroughputLower) > 1e-6 || math.Abs(upperBound-gcThroughputUpper) > 1e-6 {
		t.Fatalf("GC throughput bound should be %v, got %v.", [2]float64{gcThroughputLower, gcThroughputUpper}, [2]float64{lowerBound, upperBound})
	}
}

func checkGCThroughputInRange(t *testing.T, gcThroughput float64, scope [2]float64) {
	if gcThroughput < scope[0] || gcThroughput > scope[1] {
		t.Fatalf("gc throughput should be in range %v, got %v.", scope, gcThroughput)
	}
}

func launchSubProcess(t *testing.T, testFuncName string, envVars ...string) {
	var outb, errb bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^"+testFuncName+"$")
	cmd.Env = append(os.Environ(), envVars...)
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		t.Log(cmd.Stderr)
		t.Log(cmd.Stdout)
		t.Fatalf("Return error: %s\n", err)
	}
}

func triggerGCWorkload(times int) {
	// expect to trigger gc happen.
	gcNum := int(GetGCNum())
	for int(GetGCNum())-gcNum < times {
		gcAdaptiveSink = make([]byte, 1<<20)
	}
}

func waitForGCStable() {
	for {
		triggerGCWorkload(2)
		if IsGCStable() {
			break
		}
	}
}

// Check all env settings parsed correctly.
func TestGCAdaptiveEnvSetting(t *testing.T) {
	GOGCUPPERLIMIT := int32(150)
	GOGCTHPRATIO := int32(97)
	GOGCTHPRATIOALPHA := 70
	GOGCTHPSIGMOIDK := 5
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		// Check GOGCUPPERLIMIT
		if goGCUPPERLIMIT := ReadGOGCUPPERLIMIT(); goGCUPPERLIMIT != GOGCUPPERLIMIT {
			t.Fatalf("GOGCUPPERLIMIT should be %d, got %v.", GOGCUPPERLIMIT, goGCUPPERLIMIT)
		}
		if goGCTHPRATIO := ReadGOGCTHPRATIO(); goGCTHPRATIO != GOGCTHPRATIO {
			t.Fatalf("GOGCTHPRATIO should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIOALPHA := ReadGOGCTHPRATIOALPHA(); int(goGCTHPRATIOALPHA*100) != GOGCTHPRATIOALPHA {
			t.Fatalf("GOGCTHPRATIOALPHA should be %d, got %v.", GOGCTHPRATIOALPHA, goGCTHPRATIOALPHA)
		}
		if goSIGMOIDKVALUE := ReadSIGMOIDKVALUE(); int(goSIGMOIDKVALUE) != GOGCTHPSIGMOIDK {
			t.Fatalf("GOGCTHPSIGMOIDK should be %d, got %v.", GOGCTHPSIGMOIDK, goSIGMOIDKVALUE)
		}
		// Check whether env settings take effect.
		if goGCUPPERLIMIT := GetGCAdaptiveGOGCUpperLimit(); goGCUPPERLIMIT != int32(GOGCUPPERLIMIT) {
			t.Fatalf("GC adaptive GOGC upper limit should be %d, got %v.", GOGCUPPERLIMIT, goGCUPPERLIMIT)
		}
		if goGCTHPRATIO := GetGCThroughputGoalRangeUpper(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput upper goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoalRangeLower(); int(goGCTHPRATIO) != int(float64(int(GOGCTHPRATIO)*GOGCTHPRATIOALPHA)/100.0) {
			t.Fatalf("GC throughput lower goal should be %d, got %v.", int(float64(int(GOGCTHPRATIO)*GOGCTHPRATIOALPHA)/100.0), goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoal(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoalLatest(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput latest goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		return
	}
	launchSubProcess(t, "TestGCAdaptiveEnvSetting", "SUB_PROCESS=1",
		"GOGCUPPERLIMIT="+strconv.Itoa(int(GOGCUPPERLIMIT)),
		"GOGCTHPRATIO="+strconv.Itoa(int(GOGCTHPRATIO)),
		"GOGCTHPRATIOALPHA="+strconv.Itoa(GOGCTHPRATIOALPHA),
		"GOGCTHPSIGMOIDK="+strconv.Itoa(GOGCTHPSIGMOIDK),
		"GODEBUG=gcadaptive=1")
}

// Check gc adaptive behavior with default env settings.
func TestGCAdaptiveSettingWithDefaultEnv(t *testing.T) {
	GOGCUPPERLIMIT := int32(-1)
	GOGCTHPRATIO := int32(95)
	GOGCTHPRATIOALPHA := 70
	GOGCTHPSIGMOIDK := SigmoidKDefaultValue
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		// Check GOGCUPPERLIMIT
		if goGCUPPERLIMIT := ReadGOGCUPPERLIMIT(); goGCUPPERLIMIT != GOGCUPPERLIMIT {
			t.Fatalf("GOGCUPPERLIMIT should be %d, got %v.", GOGCUPPERLIMIT, goGCUPPERLIMIT)
		}
		if goGCTHPRATIO := ReadGOGCTHPRATIO(); goGCTHPRATIO != GOGCTHPRATIO {
			t.Fatalf("GOGCTHPRATIO should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIOALPHA := ReadGOGCTHPRATIOALPHA(); int(goGCTHPRATIOALPHA*100) != GOGCTHPRATIOALPHA {
			t.Fatalf("GOGCTHPRATIOALPHA should be %d, got %v.", GOGCTHPRATIOALPHA, goGCTHPRATIOALPHA)
		}
		if goSIGMOIDKVALUE := ReadSIGMOIDKVALUE(); int(goSIGMOIDKVALUE) != int(GOGCTHPSIGMOIDK) {
			t.Fatalf("GOGCTHPSIGMOIDK should be %v, got %v.", GOGCTHPSIGMOIDK, goSIGMOIDKVALUE)
		}
		// Check whether env settings take effect.
		if goGCUPPERLIMIT := GetGCAdaptiveGOGCUpperLimit(); goGCUPPERLIMIT != GetInitialGCPercent() {
			t.Fatalf("GC adaptive GOGC upper limit should be %d, got %v.", GetInitialGCPercent(), goGCUPPERLIMIT)
		}
		if goGCTHPRATIO := GetGCThroughputGoalRangeUpper(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput upper goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoalRangeLower(); int32(goGCTHPRATIO) != int32(float64(GOGCTHPRATIO*int32(GOGCTHPRATIOALPHA))/100.0) {
			t.Fatalf("GC throughput lower goal should be %d, got %v.", int32(float64(GOGCTHPRATIO*int32(GOGCTHPRATIOALPHA))/100.0), goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoal(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoalLatest(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput latest goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		return
	}
	launchSubProcess(t, "TestGCAdaptiveSettingWithDefaultEnv", "SUB_PROCESS=1",
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
}

// Check gc adaptive behavior with adaptive gc throughput goal disabled.
func TestGCAdaptiveWithFixedGCThroughputGoal(t *testing.T) {
	GOGCUPPERLIMIT := int32(-1)
	GOGCTHPRATIO := int32(95)
	GOGCTHPRATIOALPHA := 100
	GOGCTHPSIGMOIDK := SigmoidKDefaultValue
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		// Check GOGCUPPERLIMIT
		if goGCUPPERLIMIT := ReadGOGCUPPERLIMIT(); goGCUPPERLIMIT != GOGCUPPERLIMIT {
			t.Fatalf("GOGCUPPERLIMIT should be %d, got %v.", GOGCUPPERLIMIT, goGCUPPERLIMIT)
		}
		if goGCTHPRATIO := ReadGOGCTHPRATIO(); goGCTHPRATIO != GOGCTHPRATIO {
			t.Fatalf("GOGCTHPRATIO should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIOALPHA := ReadGOGCTHPRATIOALPHA(); int(goGCTHPRATIOALPHA*100) != GOGCTHPRATIOALPHA {
			t.Fatalf("GOGCTHPRATIOALPHA should be %d, got %v.", GOGCTHPRATIOALPHA, goGCTHPRATIOALPHA)
		}
		if goSIGMOIDKVALUE := ReadSIGMOIDKVALUE(); int(goSIGMOIDKVALUE) != int(GOGCTHPSIGMOIDK) {
			t.Fatalf("GOGCTHPSIGMOIDK should be %v, got %v.", GOGCTHPSIGMOIDK, goSIGMOIDKVALUE)
		}
		// Check whether env settings take effect.
		if goGCUPPERLIMIT := GetGCAdaptiveGOGCUpperLimit(); goGCUPPERLIMIT != GetInitialGCPercent() {
			t.Fatalf("GC adaptive GOGC upper limit should be %d, got %v.", GetInitialGCPercent(), goGCUPPERLIMIT)
		}
		if goGCTHPRATIO := GetGCThroughputGoalRangeUpper(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput upper goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoalRangeLower(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput lower goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoal(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		if goGCTHPRATIO := GetGCThroughputGoalLatest(); int32(goGCTHPRATIO) != GOGCTHPRATIO {
			t.Fatalf("GC throughput latest goal should be %d, got %v.", GOGCTHPRATIO, goGCTHPRATIO)
		}
		return
	}
	// disable adaptive gc throughput goal by setting GOGCTHPRATIOALPHA to 100.
	launchSubProcess(t, "TestGCAdaptiveWithFixedGCThroughputGoal", "SUB_PROCESS=1",
		"GOGCTHPRATIOALPHA="+strconv.Itoa(GOGCTHPRATIOALPHA),
		"GODEBUG=gcadaptive=1")
}

// This case with adaptive gc throughput goal enabled and all envs are default.
func TestGCAdaptiveBehaviorWithDefaultEnv(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		triggerGCWorkload(20)
		waitForGCStable()
		// now gc adaptive will work after gc status is stable.
		triggerGCWorkload(20)
		gcThroughputUpper := GetGCThroughputGoalRangeUpper()
		gcThroughputLower := GetGCThroughputGoalRangeLower()
		// now adaptive gc throughput is disable, so we expect that gc throughput goal range should be equal.
		if int(gcThroughputUpper*0.7) != int(gcThroughputLower) {
			t.Fatalf("GC throughput goal lower range should be 0.7 * gcThroughputLower, upper range is %v, lower range is %v.", gcThroughputUpper, gcThroughputLower)
		}
		return
	}
	if os.Getenv("SUB_PROCESS") == "2" {
		checkGCAdaptiveState(t, true)
		triggerGCWorkload(20)
		waitForGCStable()
		// now gc adaptive will work after gc status is stable.
		triggerGCWorkload(20)
		gcThroughput := GetGCThroughputGoal()
		gcThroughputLatest := GetGCThroughputGoalLatest()
		gcThroughputUpper := GetGCThroughputGoalRangeUpper()
		gcThroughputLower := GetGCThroughputGoalRangeLower()
		// now adaptive gc throughput is disable, so we expect that gc throughput goal range should be equal.
		if int(gcThroughputUpper) != int(gcThroughputLower) {
			t.Fatalf("GC throughput goal lower range should be equal to upper range, upper range is %v, lower range is %v.", gcThroughputUpper, gcThroughputLower)
		}
		checkGCThroughputInRange(t, gcThroughput, [2]float64{gcThroughputLower, gcThroughputUpper})
		checkGCThroughputInRange(t, gcThroughputLatest, [2]float64{gcThroughputLower, gcThroughputUpper})
		// here we just check gc throughput is in range.
		checkGCThroughputInRange(t, GetGCThroughputRatioEMA(), [2]float64{10.0, 94.0})
		return
	}
	launchSubProcess(t, "TestGCAdaptiveBehaviorWithDefaultEnv", "SUB_PROCESS=1",
		"GOMAXPROCS=4",
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
	launchSubProcess(t, "TestGCAdaptiveBehaviorWithDefaultEnv", "SUB_PROCESS=2",
		"GOMAXPROCS=4",
		"GOGCTHPRATIOALPHA=100",
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
}

// Case one: defualt env with gc adaptive is on and GOGCUPPERLIMIT = 150
func TestBehaviorWithGCThroughputGoal(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{30, ReadGOGCUPPERLIMIT()})
		checkGCThroughputBoundRange(t, 95.7125, 94.2875, GetGCThroughputGoalToleranceUpper(), GetGCThroughputGoalToleranceLower())
		waitForGCStable()
		// expect to trigger gc happen.
		triggerGCWorkload(20)
		checkGCPercent(t, 100, GetInitialGCPercent())
		checkGCPercent(t, ReadGOGCUPPERLIMIT(), GetGCPercent())
		checkMemoryLimit(t, maxInt64, GetInitialMemoryLimit())
		return
	}
	if os.Getenv("SUB_PROCESS") == "2" {
		checkGCAdaptiveState(t, true)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{30, ReadGOGCUPPERLIMIT()})
		checkGCThroughputBoundRange(t, 95.0, 66.5, GetGCThroughputGoalRangeUpper(), GetGCThroughputGoalRangeLower())
		waitForGCStable()
		// expect to trigger gc happen.
		triggerGCWorkload(20)
		checkGCPercent(t, 100, GetInitialGCPercent())
		checkGCPercent(t, ReadGOGCUPPERLIMIT(), GetGCPercent())
		checkMemoryLimit(t, maxInt64, GetInitialMemoryLimit())
		return
	}
	launchSubProcess(t, "TestBehaviorWithGCThroughputGoal", "SUB_PROCESS=1",
		"GOMAXPROCS=4",
		"GOGCUPPERLIMIT=150",
		"GOGCTHPRATIOALPHA=100",
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
	launchSubProcess(t, "TestBehaviorWithGCThroughputGoal", "SUB_PROCESS=2",
		"GOMAXPROCS=4",
		"GOGCUPPERLIMIT=150",
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
}

// Case two: GOGC=off with gc adaptive is on and GOGCUPPERLIMIT = 150
func TestGCAdaptiveWithGOGCOff(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{30, ReadGOGCUPPERLIMIT()})
		checkGCThroughputBoundRange(t, 95.7125, 94.2875, GetGCThroughputGoalToleranceUpper(), GetGCThroughputGoalToleranceLower())
		checkGCPercent(t, GetGCAdaptiveDefaultGCPercent(), GetGCPercent())
		waitForGCStable()
		// expect to trigger gc happen.
		triggerGCWorkload(20)
		checkGCPercent(t, -1, GetInitialGCPercent())
		checkGCPercent(t, GetGCPercent(), ReadGOGCUPPERLIMIT())
		checkGCThroughputInRange(t, GetGCThroughputRatioEMA(), [2]float64{10.0, 94.0})
		return
	}
	if os.Getenv("SUB_PROCESS") == "2" {
		checkGCAdaptiveState(t, true)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{30, ReadGOGCUPPERLIMIT()})
		checkGCThroughputBoundRange(t, 95.0, 66.5, GetGCThroughputGoalRangeUpper(), GetGCThroughputGoalRangeLower())
		checkGCPercent(t, GetGCAdaptiveDefaultGCPercent(), GetGCPercent())
		waitForGCStable()
		// expect to trigger gc happen.
		triggerGCWorkload(20)
		checkGCPercent(t, -1, GetInitialGCPercent())
		checkGCPercent(t, GetGCPercent(), ReadGOGCUPPERLIMIT())
		checkGCThroughputInRange(t, GetGCThroughputRatioEMA(), [2]float64{10.0, 94.0})
		return
	}
	launchSubProcess(t, "TestGCAdaptiveWithGOGCOff", "SUB_PROCESS=1",
		"GOMAXPROCS=4",
		"GOGCUPPERLIMIT=150",
		"GOGCTHPRATIOALPHA=100",
		"GOGC=off",
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
	launchSubProcess(t, "TestGCAdaptiveWithGOGCOff", "SUB_PROCESS=2",
		"GOMAXPROCS=4",
		"GOGCUPPERLIMIT=150",
		"GOGC=off",
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
}

func TestGCAdaptiveSwitch(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		// Check the initial state of GC adaptive
		checkGCAdaptiveState(t, false)
		checkGCPercent(t, 400, GetGCPercent())
		checkGCPercent(t, 400, GetInitialGCPercent())
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{30, 400})
		checkGCThroughputBoundRange(t, 60.45, 59.55, GetGCThroughputGoalToleranceUpper(), GetGCThroughputGoalToleranceLower())
		// Enable GC adaptive.
		SetGCAdaptiveEnabled(1)
		checkGCAdaptiveState(t, true)
		// Make sure the GC state is stable.
		waitForGCStable()
		triggerGCWorkload(15)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GetGCAdaptiveGOGCLowerLimit(), GetGCAdaptiveGOGCUpperLimit()})
		SetGCThroughputGoal(95.0)
		triggerGCWorkload(1)
		checkGCThroughputBoundRange(t, 95.0, 95.0*GetGCThroughputGoalRangeAlpha(), GetGCThroughputGoalRangeUpper(), GetGCThroughputGoalRangeLower())
		SetGCThroughputGoalRange(95.0, 1.0)
		// Make sure gc througput is updated.
		triggerGCWorkload(1)
		checkGCThroughputBoundRange(t, 95.0, 95.0, GetGCThroughputGoalRangeUpper(), GetGCThroughputGoalRangeLower())
		triggerGCWorkload(30)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GetGCAdaptiveGOGCUpperLimit() - 100, GetGCAdaptiveGOGCUpperLimit()})
		SetGCAdaptiveEnabled(0)
		checkGCAdaptiveState(t, false)
		// GOGC should be restore to 400.
		checkGCPercent(t, 400, GetGCPercent())
		return
	}
	if os.Getenv("SUB_PROCESS") == "2" {
		// Check the initial state of GC adaptive
		checkGCAdaptiveState(t, false)
		checkGCPercent(t, 400, GetGCPercent())
		checkGCPercent(t, 400, GetInitialGCPercent())
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{30, 400})
		checkGCThroughputBoundRange(t, 95.0, 66.5, GetGCThroughputGoalRangeUpper(), GetGCThroughputGoalRangeLower())
		// Enable GC adaptive.
		SetGCAdaptiveEnabled(1)
		checkGCAdaptiveState(t, true)
		// Make sure the GC state is stable.
		waitForGCStable()
		triggerGCWorkload(15)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GetGCAdaptiveGOGCLowerLimit(), GetGCAdaptiveGOGCUpperLimit()})
		SetGCThroughputGoalRange(97.0, 0.6)
		// Make sure gc througput is updated.
		triggerGCWorkload(1)
		checkGCThroughputBoundRange(t, 97.0, 97.0*0.6, GetGCThroughputGoalRangeUpper(), GetGCThroughputGoalRangeLower())
		triggerGCWorkload(30)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GetGCAdaptiveGOGCUpperLimit() - 100, GetGCAdaptiveGOGCUpperLimit()})
		SetGCAdaptiveEnabled(0)
		checkGCAdaptiveState(t, false)
		// GOGC should be restore to 400.
		checkGCPercent(t, 400, GetGCPercent())
		return
	}
	launchSubProcess(t, "TestGCAdaptiveSwitch", "SUB_PROCESS=1",
		"GOMAXPROCS=4",
		"GOGC=400",
		"GOGCTHPRATIO=60",
		"GOGCTHPRATIOALPHA=100",
		"GODEBUG=gctrace=1,gcadaptivetrace=1")
	launchSubProcess(t, "TestGCAdaptiveSwitch", "SUB_PROCESS=2",
		"GOMAXPROCS=4",
		"GOGC=400",
		"GODEBUG=gctrace=1,gcadaptivetrace=1")
}

func TestGCAdaptiveWithMaxHeapOn(t *testing.T) {
	if os.Getenv("SUB_PROCESS") == "1" {
		checkGCAdaptiveState(t, true)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{30, 100})
		checkGCThroughputBoundRange(t, 95.7125, 94.2875, GetGCThroughputGoalToleranceUpper(), GetGCThroughputGoalToleranceLower())
		checkGCPercent(t, 100, GetGCPercent())
		// Make sure the GC state is stable.
		waitForGCStable()
		triggerGCWorkload(5)
		checkGCPercent(t, 100, GetInitialGCPercent())
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GetGCAdaptiveDefaultGCPercent(), 100000})
		return
	}
	if os.Getenv("SUB_PROCESS") == "2" {
		checkGCAdaptiveState(t, true)
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{30, 100})
		checkGCThroughputBoundRange(t, 95.0, 66.5, GetGCThroughputGoalRangeUpper(), GetGCThroughputGoalRangeLower())
		checkGCPercent(t, 100, GetGCPercent())
		// Make sure the GC state is stable.
		waitForGCStable()
		triggerGCWorkload(5)
		checkGCPercent(t, 100, GetInitialGCPercent())
		checkGCPercentInRange(t, GetGCPercent(), [2]int32{GetGCAdaptiveDefaultGCPercent(), 100000})
		return
	}
	launchSubProcess(t, "TestGCAdaptiveWithMaxHeapOn", "SUB_PROCESS=1",
		"GOMAXPROCS=4",
		"GOGCTHPRATIOALPHA=100",
		"MY_MEM_LIMIT="+strconv.Itoa(400<<20),
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
	launchSubProcess(t, "TestGCAdaptiveWithMaxHeapOn", "SUB_PROCESS=2",
		"GOMAXPROCS=4",
		"MY_MEM_LIMIT="+strconv.Itoa(400<<20),
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1")
}
