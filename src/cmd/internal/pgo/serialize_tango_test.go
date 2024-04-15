// Copyright 2024 The Go Authors. All rights reserved.
// Copyright (c) 2024 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2024-04-15.

package pgo

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

func TestRoundTripWithTangoInfo(t *testing.T) {
	d := &Profile{
		TotalWeight: 3,
		TangoInfo: map[string]string{
			"StackSize": "32768",
		},
		NamedEdgeMap: NamedEdgeMap{
			ByWeight: []NamedCallEdge{
				{
					CallerName:     "a",
					CalleeName:     "b",
					CallSiteOffset: 14,
				},
				{
					CallerName:     "c",
					CalleeName:     "d",
					CallSiteOffset: 15,
				},
			},
			Weight: map[NamedCallEdge]int64{
				{
					CallerName:     "a",
					CalleeName:     "b",
					CallSiteOffset: 14,
				}: 2,
				{
					CallerName:     "c",
					CalleeName:     "d",
					CallSiteOffset: 15,
				}: 1,
			},
		},
	}

	testRoundTripWithTangoInfo(t, d)
}

func testRoundTripWithTangoInfo(t *testing.T, d *Profile) []byte {
	var buf bytes.Buffer
	n, err := d.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo got err %v want nil", err)
	}
	if n != int64(buf.Len()) {
		t.Errorf("WriteTo got n %d want %d", n, int64(buf.Len()))
	}

	b := buf.Bytes()

	got, err := FromSerialized(&buf)
	if err != nil {
		t.Fatalf("processSerialized got err %v want nil", err)
	}
	if err := equalWithTangoInfo(got, d); err != nil {
		t.Errorf("processSerialized output does not match input: %v", err)
	}

	return b
}

// equal returns an error if got and want are not equal.
func equalWithTangoInfo(got, want *Profile) error {
	if got.TotalWeight != want.TotalWeight {
		return fmt.Errorf("got.TotalWeight %d != want.TotalWeight %d", got.TotalWeight, want.TotalWeight)
	}
	if !reflect.DeepEqual(got.NamedEdgeMap.ByWeight, want.NamedEdgeMap.ByWeight) {
		return fmt.Errorf("got.NamedEdgeMap.ByWeight != want.NamedEdgeMap.ByWeight\ngot = %+v\nwant = %+v", got.NamedEdgeMap.ByWeight, want.NamedEdgeMap.ByWeight)
	}
	if !reflect.DeepEqual(got.NamedEdgeMap.Weight, want.NamedEdgeMap.Weight) {
		return fmt.Errorf("got.NamedEdgeMap.Weight != want.NamedEdgeMap.Weight\ngot = %+v\nwant = %+v", got.NamedEdgeMap.Weight, want.NamedEdgeMap.Weight)
	}
	if !reflect.DeepEqual(got.TangoInfo, want.TangoInfo) {
		return fmt.Errorf("got.TangoInfo != want.TangoInfo\ngot = %+v\nwant = %+v", got.TangoInfo, want.TangoInfo)
	}

	return nil
}
