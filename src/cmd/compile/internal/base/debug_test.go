// Copyright 2026 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package base

import (
	"reflect"
	"testing"
)

// nonConcurrentDebugFlags contains the existing exceptions to the rule that
// compiler debug flags must support concurrent backend compilation. New
// exceptions must be added explicitly after confirming that concurrent
// compilation is unsafe.
var nonConcurrentDebugFlags = map[string]struct{}{
	"Append":                {},
	"Closure":               {},
	"CompressInstructions":  {},
	"Defer":                 {},
	"DumpInlFuncProps":      {},
	"DumpInlCallSiteScores": {},
	"InlScoreAdj":           {},
	"DumpPtrs":              {},
	"DwarfInl":              {},
	"Export":                {},
	"GCProg":                {},
	"Gossahash":             {},
	"Libfuzzer":             {},
	"LoopVar":               {},
	"LoopVarHash":           {},
	"LocationLists":         {},
	"MergeLocalsDumpFunc":   {},
	"MergeLocalsTrace":      {},
	"MergeLocalsHTrace":     {},
	"Nil":                   {},
	"PCTab":                 {},
	"Panic":                 {},
	"Reshape":               {},
	"Shapify":               {},
	"Slice":                 {},
	"SyncFrames":            {},
	"TailCall":              {},
	"TypeAssert":            {},
	"WB":                    {},
	"ABIWrap":               {},
	"PGODebug":              {},
	"WrapGlobalMapDbg":      {},
	"WrapGlobalMapCtl":      {},
}

func TestDebugFlagsConcurrent(t *testing.T) {
	typ := reflect.TypeOf(DebugFlags{})
	seen := make(map[string]bool)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Name == "ConcurrentOk" {
			continue
		}
		seen[field.Name] = true

		_, exception := nonConcurrentDebugFlags[field.Name]
		if field.Tag.Get("concurrent") == "ok" {
			if exception {
				t.Errorf("DebugFlags.%s supports concurrent compilation; remove it from nonConcurrentDebugFlags", field.Name)
			}
			continue
		}
		if !exception {
			t.Errorf("DebugFlags.%s must be marked concurrent:%q or registered in nonConcurrentDebugFlags", field.Name, "ok")
		}
	}

	for name := range nonConcurrentDebugFlags {
		if !seen[name] {
			t.Errorf("nonConcurrentDebugFlags contains unknown field %q", name)
		}
	}
}
