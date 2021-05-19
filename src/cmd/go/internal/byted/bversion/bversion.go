// Copyright 2020 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package bversion implements the “go bversion” command.
package bversion

import (
	"cmd/go/internal/base"
	"context"
	"fmt"
	"runtime"
)

var CmdBVersion = &base.Command{
	UsageLine:   "go bversion",
	CustomFlags: true,
	Short:       "print Bytedance Go version",
	Long:        "BVersion prints the Bytedance Go version.",
}

func init() {
	CmdBVersion.Run = runBVersion // break init cycle
}

func runBVersion(ctx context.Context, cmd *base.Command, args []string) {
	fmt.Printf("Tango bversion %s %s/%s\n", runtime.BVersion(), runtime.GOOS, runtime.GOARCH)
}
