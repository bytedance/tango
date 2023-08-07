// Copyright 2023 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package load

import (
	"reflect"
	"testing"
)

type pgoSourceTest struct {
	filename string
	wantInfo pgoSourceInfo
	wantOK   bool
}

var pgoSourceTests = []pgoSourceTest{
	{
		"",
		pgoSourceInfo{},
		false,
	},
	{
		"...",
		pgoSourceInfo{},
		false,
	},
	{
		"default.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source.default.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source.global.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source.global.service.profile_202308042359.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source..service.profile_202308042359_xxxxx1.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source.global..._202308042359_xxxxx1.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source.global.profile_20230804_xxxxx1.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source.global.service.profile_202308042359xxxxx1.pgo",
		pgoSourceInfo{},
		false,
	},
	{
		"source.global.profile_202308042359_xxxxx1.pgo",
		pgoSourceInfo{
			"global",
			[]string{
				"profile_202308042359_xxxxx1",
			}},
		true,
	},
	{
		"source.global.service.profile_202308042359_xxxxx1.pgo",
		pgoSourceInfo{
			"global",
			[]string{
				"service.profile_202308042359_xxxxx1",
			}},
		true,
	},
	{
		"source.global.service.profile_202308042359_xxxxx1_2359_xxxxx2_2359_xxxxx3.pgo",
		pgoSourceInfo{
			"global",
			[]string{
				"service.profile_202308042359_xxxxx1",
				"service.profile_202308042359_xxxxx2",
				"service.profile_202308042359_xxxxx3",
			}},
		true,
	},
}

func TestParsePGOSourceInfo(t *testing.T) {
	for _, tt := range pgoSourceTests {
		gotInfo, gotOK := parsePGOSourceInfo(tt.filename)
		if gotOK != tt.wantOK || !reflect.DeepEqual(gotInfo, tt.wantInfo) {
			t.Errorf("parsePGOSourceInfo(%s) = (info=%#v, ok=%v); want (info=%#v, ok=%v)", tt.filename, gotInfo, gotOK, tt.wantInfo, tt.wantOK)
		}
	}
}
