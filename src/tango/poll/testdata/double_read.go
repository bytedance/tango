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

// Test: Waiting on the same poll descriptor with same mode is forbidden.
func main() {
	epfd, epd := createEpoll()
	defer syscall.Close(epfd)
	defer unregister(epd)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := waitRead(epd); err != nil {
			fmt.Printf("error: %v\n", err)
			os.Exit(1)
		}
	}()

	go func() {
		defer wg.Done()
		if err := waitRead(epd); err != nil {
			fmt.Printf("error: %v\n", err)
			os.Exit(1)
		}
	}()

	wg.Wait()
}
