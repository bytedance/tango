// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package main to move policy tests into separated process
// to mitigate the impact of other concurrently running testcases.
package main

import (
	"os"
	"strings"
	_ "unsafe"
)

var (
	cmds             = map[string]func(){}
	gab              *int32
	gabEnabled       *bool
	gabUsedBytes     *uintptr
	gabMemoryPercent *int32
	gabThreshold     *uint64
)

//go:linkname getGabCap runtime.gabGetCap
func getGabCap() (init, max uintptr)

//go:linkname rtCfgListAll runtime.rtCfgListAll
func rtCfgListAll() map[string]any

//go:linkname isInGabSpan runtime.dbgIsInGabSpan
func isInGabSpan(p uintptr) bool

//go:linkname gabPin runtime.dbgGabPin
func gabPin(p uintptr)

//go:linkname isGabPinned runtime.dbgIsGabPinned
func isGabPinned(p uintptr) bool

//go:linkname dumpGABObject runtime.dbgDumpGABObject
func dumpGABObject(label string, p uintptr) bool

//go:linkname makeGabStrHeaderForWBTest runtime.dbgMakeGabStrHeaderForWBTest
func makeGabStrHeaderForWBTest() (bodyPtr, headerPtr uintptr)

//go:linkname gabStrHeaderBodyAddrNeedsMallocHeader runtime.dbgGabStrHeaderBodyAddrNeedsMallocHeader
func gabStrHeaderBodyAddrNeedsMallocHeader(bodyPtr, headerPtr uintptr) bool

func register(name string, f func()) {
	if cmds[name] != nil {
		panic("duplicate registration: " + name)
	}
	cmds[name] = f
}

func registerInit(name string, f func()) {
	if len(os.Args) >= 2 && os.Args[1] == name {
		f()
	}
}

func init() {
	configs := rtCfgListAll()
	if len(configs) == 0 {
		panic("no configs found")
	}
	for key, value := range configs {
		if strings.HasPrefix(key, "runtime.gabUsedBytes,") {
			gabUsedBytes = value.(*uintptr)
		} else if strings.HasPrefix(key, "runtime.gabMemoryPercent,") {
			gabMemoryPercent = value.(*int32)
		} else if strings.HasPrefix(key, "runtime.gab,") {
			gab = value.(*int32)
		} else if strings.HasPrefix(key, "runtime.gabEnabled,") {
			gabEnabled = value.(*bool)
		} else if strings.HasPrefix(key, "runtime.gabThreshold,") {
			gabThreshold = value.(*uint64)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		println("usage: " + os.Args[0] + " name-of-test")
		return
	}
	f := cmds[os.Args[1]]
	if f == nil {
		println("unknown function: " + os.Args[1])
		return
	}
	f()
}
