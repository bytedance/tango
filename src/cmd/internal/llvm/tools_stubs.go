// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !goexperiment.tangollvm || !linux || !amd64 || !cgo || compiler_bootstrap

package llvm

import "errors"

type Tools struct {
}

func DefaultTools(verbose int) (*Tools, error) {
	return &Tools{}, errors.New("TangoLLVM only support Linux/amd64 now")
}

func (t *Tools) SetDir(dir string) {}

func (t *Tools) RunLTO(archives []string) {}

func DefaultLibraryPath() string {
	return ""
}
