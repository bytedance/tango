// Copyright 2014 The Go Authors. All rights reserved.
// Copyright (c) 2021 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2021-10-25.

package runtime

//go:nowritebarrierrec
func (s *mspan) isGab() bool {
	return s.spanclass >= numNonGabSpanClasses
}
