// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

// Test: waiting to the same poll descriptor with same mode is forbidden.
func main() {
	epfd, pd := createEpoll()
	defer unregister(pd)
	defer syscall.Close(epfd)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := waitWrite(pd); err != nil {
			fmt.Printf("error: %v\n", err)
			os.Exit(1)
		}
	}()

	go func() {
		defer wg.Done()
		waitWrite(pd)
		if err := waitWrite(pd); err != nil {
			fmt.Printf("error: %v\n", err)
			os.Exit(1)
		}
	}()

	wg.Wait()
}
