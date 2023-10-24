// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dynconf

import (
	"path"
	"runtime/debug"
	"sync"
	_ "unsafe"
)

//go:linkname listRuntimeConfigs runtime.rtCfgListAll
func listRuntimeConfigs() map[string]any

// gcSetMaxHeap is provided by package runtime.
//
//go:linkname gcSetMaxHeap runtime.gcSetMaxHeap
func gcSetMaxHeap(bytes int64) int64

//go:linkname setPGOSource runtime.setPGOSource
func setPGOSource(value string)

var once sync.Once

func initPGOSource() {
	once.Do(func() {
		setPGOSource(pgoSourceFromBuildInfo())
	})
}

func pgoSourceFromBuildInfo() string {
	const pgoFlag = "-pgo"
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "none"
	}
	for _, setting := range buildInfo.Settings {
		if setting.Key == pgoFlag {
			// buildInfo value contains the full path of pgo source file
			// we only need to show the base filename to user
			return path.Base(setting.Value)
		}
	}
	return "none"
}
