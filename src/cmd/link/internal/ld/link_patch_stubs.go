// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !goexperiment.tangollvm || !linux || !amd64 || !cgo || compiler_bootstrap

package ld

func patchSymbols(*Link) {}
