// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !compilerbeast
// +build !compilerbeast

package runtime

const (
	// The minimum size of stack used by Go code
	stackMin = 2048
	optmode  = "default"
)
