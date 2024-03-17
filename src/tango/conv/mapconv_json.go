// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !goexperiment.jsonv2

package conv

import (
	"reflect"
	_ "unsafe"
)

//go:linkname extractStructFields encoding/json.tcExtractStructFields
func extractStructFields(t reflect.Type) ([]bool, [][]int, []string, []bool, []reflect.Type, []bool, []bool)

//go:linkname stringBytes encoding/json.tcStringBytes
func stringBytes(s []byte) (res []byte)

//go:linkname parseInterface encoding/json.tcParseInterface
func parseInterface(b []byte, useNumber bool) any
