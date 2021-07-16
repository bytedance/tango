// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && amd64
// +build linux,amd64

package main

import (
	"syscall"
)

// Test: unregistering a poll descriptor more than once is forbidden
func main() {
	epfd, epd := createEpoll()
	defer syscall.Close(epfd)
	unregister(epd)
	unregister(epd)
}
