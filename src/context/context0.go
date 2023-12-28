// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !compilerbeast
// +build !compilerbeast

package context

func value(c Context, key any) any {
	return valueDefault(c, key)
}
