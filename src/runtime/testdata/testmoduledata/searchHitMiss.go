// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"reflect"
	"runtime"
	"strconv"
)

func init() {
	register("ModuleDataOptHitMiss", moduleDataOptHitMiss)
}

func moduleDataOptHitMiss() {
	count := 1
	if value := os.Getenv("MODULEDATACOUNT"); value != "" {
		if c, err := strconv.Atoi(value); err == nil {
			count = c
		}
	}
	clean, err := Init(count)
	if clean != nil {
		defer clean()
	}
	if err != nil {
		println("[Main] err: init plugin failed")
		return
	}
	for i := 1; i <= count; i++ {
		err = loadAndRunPlugin(tmpDir, i)
		if err != nil {
			println("[Main] err: load plugin error", err.Error())
			return
		}
	}
	if err = checkLastPlugin(); err != nil {
		println("[Main] err: checkLastPlugin failed")
		return
	}
	runtime.GC()
	println("[Main] update to hit")
	runtime.GC()
	entry := runtime.FuncForPC(reflect.ValueOf(globalPluginFunc).Pointer()).Entry()
	for i := 0; i < 100; i++ {
		_ = runtime.FuncForPC(reflect.ValueOf(globalPluginFunc).Pointer()).Name()
		_ = reflect.TypeOf(reflect.New(reflect.TypeOf(globalPluginType).Elem()).Interface()).String()
	}
	runtime.GC()
	println("[Main] update to miss")
	f := runtime.FuncForPC(entry + 800)
	if f != nil {
		println("[Main] err: get not exist func failed")
	}
	runtime.GC()
}
