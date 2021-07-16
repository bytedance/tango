// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64
// +build amd64

package poll

import (
	"syscall"
	"unsafe"
)

// Poll_tango_reg creates a runtime polling descriptor for file fd.
// fd should represent a pollable file.
func Poll_tango_reg(fd uintptr) (unsafe.Pointer, error) {
	serverInit.Do(runtime_pollServerInit)
	pd, errno := runtime_pollOpen(fd)
	if errno != 0 {
		if pd != 0 {
			runtime_pollUnblock(pd)
			runtime_pollClose(pd)
		}
		return nil, errnoErr(syscall.Errno(errno))
	}
	runtime_tangoPollEnable()
	return unsafe.Pointer(pd), nil
}

// Poll_tango_wait implements waiting for a descriptor to be ready.
// 'mode' should be either 'r' or 'w', representing 'reading' or
// 'writing' events respectively.
func Poll_tango_wait(pd unsafe.Pointer, mode int) error {
	if pd == nil {
		panic("nil poll descriptor")
	}
	if r := runtime_pollWait(uintptr(pd), mode); r != pollNoError {
		return convertErr(r, true)
	}
	return nil
}

// Poll_tango_unreg removes a polling descriptor registered in runtime poller.
// unregistering an invalid or previously-closed descriptor may cause panic.
func Poll_tango_unreg(pd unsafe.Pointer) {
	if pd == nil {
		return
	}
	runtime_pollUnblock(uintptr(pd))
	runtime_pollClose(uintptr(pd))
}

// runtime_tangoPollEnable notifies runtime that current goroutine is
// registering an external pollable file to its integrated netpoller
func runtime_tangoPollEnable()
