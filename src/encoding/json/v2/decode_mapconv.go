// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.jsonv2

package json

import (
	"encoding/json/internal/jsonflags"
	_ "unsafe"
)

//go:linkname tcParseInterface
func tcParseInterface(b []byte, useNumber bool) any {
	opt := jsonflags.UnmarshalAnyWithRawNumber
	if useNumber {
		opt = opt | 1
	}
	dec := export.GetBufferedDecoder(b, []Options{opt}...)
	defer export.PutBufferedDecoder(dec)
	xd := export.Decoder(dec)
	var out interface{}
	err := unmarshalDecode(dec, &out, &xd.Struct, true)
	if err != nil {
		return nil
	}
	return out
}
