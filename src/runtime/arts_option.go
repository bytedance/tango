// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adaptive Runtime Scheduling

package runtime

const (
	_AdaptOptAll = 1 << iota
	_AdaptOptGstack
	_AdaptOptPoller
	_AdaptOptBgsweep
	// The optimization to working steal is not needed in 1.17 and above,
	// as a recent refactoring to the runtime scheduler covers it, but we
	// the enum for consistency.
	_AdaptOptSteal
	_AdaptOptSysmon
)

var (
	// adaptive runtime scheduling may decrease the longest delay that sysmon
	// checks if network polling should be performed, in order to do so, we
	// make the original delay thresholds variables.
	//
	// Shortening the delay periods have both pros and cons, generally it improves
	// the runtime's responsive to networking events under low workload at
	// a cost of slight cpu usage increase, we expect such increase is minimal
	// and, most of the time, invisible.

	// sysmon's default delay threshold, 10ms (10*1000 us)
	sysmonDelayThreshold uint32 = 10 * 1000
	// sysmon's default netpolling period, 10ms (10*1000*1000 ns)
	sysmonNetpollThreshold int64 = 10 * 1000 * 1000

	// scale down the sysmon's default thresholds to its 10-th with the
	// adaptive scheduling mode.
	// '10' is an empirical number.
	sysmonThresholdScaledown int32 = 10

	// adaptive runtime scheduling optimizations currently enabled, for dynconf
	artsOpts string
)

type artsOpt struct {
	abbr string
	desc string
	mask int32
}

// adaptive runtime scheduling optimizations.
// turned on by GODEBUG=adapt=<mask>, recommend to set 'adapt=1' if no specific concern.
var artsOptTbl []artsOpt = []artsOpt{
	{"all", "all adaptive runtime scheduler optimizations", _AdaptOptAll},
	{"gstack", "goroutine and goroutine stack optimization", _AdaptOptGstack},
	{"poller", "poller goroutine optimization", _AdaptOptPoller},
	{"bgsweep", "background sweeping optimization", _AdaptOptBgsweep},
	{"steal", "runtime steal optimization", _AdaptOptSteal},
	{"sysmon", "higher frequency of system monitor", _AdaptOptSysmon},
}

// artsInit initialize the adaptive runtime scheduling mode.
// for now, it's expected to be part of the bootstrap sequence.
// It's possible that GODEBUG=adapttrace=<N> is specified while
// GODEBUG=adapt=<N> not (N is positive), we allow the usage for
// better diagnostic, but it's subject to change later.
func artsInit() {
	if artsOptEnabled(_AdaptOptSysmon) {
		sysmonDelayThreshold /= uint32(sysmonThresholdScaledown)
		sysmonNetpollThreshold /= int64(sysmonThresholdScaledown)
	}
	artsOpts = artsOptStr()
}

// artsOptStr returns the adaptive runtime scheduling optimizations currently enabled
func artsOptStr() string {
	res := "["
	first := true

	for _, opt := range artsOptTbl {
		if debug.adapt&opt.mask != 0 {
			if !first {
				res += ":"
			}
			res += opt.abbr
			first = false
		}
	}

	res += "]"
	if res == "[]" {
		res = "none"
	}
	return res
}

// artsOptEnabled checks if any specific optimization is enabled or not, the parameter
// 'opt' is expected to be one of the masks defined above (_AdaptOpt*) or their
// combination.
// For performance sake, sanity checking to the parameter 'opt' is relieved.
func artsOptEnabled(opt int32) bool {
	return debug.adapt&(opt|_AdaptOptAll) != 0
}

// artsTraceEnabled checks whether the internal tracing log no less than level 'l' is turned on
func artsTraceEnabled(l int32) bool {
	return debug.adapttrace >= l
}
