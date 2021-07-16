// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	"fmt"
	"syscall"
	"time"
)

// Test: empty interest list yields no notification
func main() {
	epfd, pd := createEpoll()
	defer unregister(pd)
	defer syscall.Close(epfd)

	waken := make(chan string)

	go func() {
		if err := waitRead(pd); err != nil {
			// expected unblocking
			return
		}
		waken <- "read ready"
	}()

	go func() {
		if err := waitWrite(pd); err != nil {
			// expected unblocking
			return
		}
		waken <- "write ready"
	}()

	timer := time.NewTimer(2 * time.Second)
	select {
	case <-timer.C:
		fmt.Printf("Polling timed out as expected\n")
	case w := <-waken:
		fmt.Printf("Unexpected waking up, message: %s\n", w)
	}
}
