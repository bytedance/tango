// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package poll implements a new synchronization mechanism that monitors multiple
// (registered) external file descriptors and wakes up their goroutines if I/O
// is possible on any of them.
//
// It aims to help reduce the scheduling latency of user goroutines which have
// to manage I/O event notification in their own way without benefiting from
// the runtime's integrated netpolling.
//
// The new mechanism relies on linux epoll and runtime's integrated netpolling
// under the hood, thereby the external file descriptor under monitoring could
// be an epoll instance which watches its own interest list in case your linux
// kernel supports nested epoll.
//
// We leverage the whole existing flow of runtime's integrated polling as it's
// already production-proven, while have to take in its strict error handling,
// most of illegal scenarios panic runtime, please refer to the various tests
// under testdata for details.
//
// Be aware that the integrated netpolling of runtime is edge-triggered on
// linux, user code should handle the event notification in reasonable
// way.
//
// We'll make it available on other architectures after evaluation.

//go:build amd64
// +build amd64

package poll

import (
	ipoll "internal/poll"
	"unsafe"
)

// register puts an external file 'fd' under monitoring and returns a 'poll
// descriptor' as its identity.
// 'fd' must designate a pollable file (TODO: strict checking if necessary).
//
//go:linkname register
func register(fd int) (unsafe.Pointer, error) {
	return ipoll.Poll_tango_reg(uintptr(fd))
}

// unregister cleans up the internal poll stuff associated with 'pd'.
// Internally it may 'ready' the parked goroutine waiting for events on the
// external file designated by 'pd' and removes the external file from the
// interest list of runtime's epoll instance.
// User should ensure the external file designated by 'pd' stay valid.
//
//go:linkname unregister
func unregister(pd unsafe.Pointer) {
	ipoll.Poll_tango_unreg(pd)
}

// waitRead waits for readable events on the external file designated by 'pd'.
// The 'reader' goroutine parks and waits for events to become available then
// unparks to proceed.
// Multiple waitRead on the same 'pd' concurrently is forbidden.
//
//go:linkname waitRead
func waitRead(pd unsafe.Pointer) error {
	return ipoll.Poll_tango_wait(pd, 'r')
}

// WaitWrite waits for writable events on the external file designated by 'pd'.
// The 'writer' goroutine parks and wait for events to become available then
// unparks to proceed.
// Multiple waitWrite on the same 'pd' concurrently is forbidden.
//
//go:linkname waitWrite
func waitWrite(pd unsafe.Pointer) error {
	return ipoll.Poll_tango_wait(pd, 'w')
}
