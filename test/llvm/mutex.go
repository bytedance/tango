// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"sync"
)

func main() {
	wgTest()
	MutexTest()
}

var x int32 = 0

//go:noinline
func increment(wg *sync.WaitGroup, m *sync.Mutex) {
	m.Lock()
	x = x + 1
	m.Unlock()
	wg.Done()
}

//go:noinline
func wgTest() {
	var w sync.WaitGroup
	m := new(sync.Mutex)
	for i := 0; i < 5000; i++ {
		w.Add(1)
		go increment(&w, m)
	}
	w.Wait()
	if x != 5000 {
		panic("wait group not work or mutex not work")
	}
}

//go:noinline
func MutexTest() {
	m := new(sync.Mutex)

	m.Lock()
	if m.TryLock() {
		panic("TryLock succeeded with mutex locked")
	}
	m.Unlock()
	if !m.TryLock() {
		panic("TryLock failed with mutex unlocked")
	}
	m.Unlock()

}
