// Copyright 2010 The Go Authors. All rights reserved.
// Copyright (c) 2023 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2023-04-24.

//go:build !goexperiment.jsonv2

package json

import (
	"reflect"
	_ "unsafe"
)

const tcMaxVisitedCount = 30

// tcExtractStructFields reference typeFields, and we reduce the code for handling fields with the same name,
// the code for doing sort. Also, we change the code for handling "-".
//
//go:linkname tcExtractStructFields
func tcExtractStructFields(t reflect.Type) ([]bool, [][]int, []string, []bool) {
	// Anonymous fields to explore at the current level and the next.
	var currentType []reflect.Type
	var currentIndex [][]int
	nextType := []reflect.Type{t}
	nextIndex := make([][]int, 1)

	// Types already visited at an earlier level.
	visited := map[reflect.Type]int{}

	// Fields found.
	var omitemptyRes []bool
	var indexRes [][]int
	var nameRes []string
	var exportRes []bool

	for len(nextType) > 0 {
		currentType, nextType = nextType, currentType[:0]
		currentIndex, nextIndex = nextIndex, currentIndex[:0]

		for idx, f := range currentType {
			if visited[f] > tcMaxVisitedCount {
				continue
			}
			visited[f]++

			// Scan f for fields to include.
			for i := 0; i < f.NumField(); i++ {
				sf := f.Field(i)
				tag := sf.Tag.Get("json")
				name, opts := parseTag(tag)
				if !isValidTag(name) {
					name = ""
				}
				export := sf.IsExported()
				if name == "-" {
					name = sf.Name
					export = false
				}
				index := make([]int, len(currentIndex[idx])+1)
				copy(index, currentIndex[idx])
				index[len(currentIndex[idx])] = i

				ft := sf.Type
				if ft.Name() == "" && ft.Kind() == reflect.Pointer {
					// Follow pointer.
					ft = ft.Elem()
				}

				// Record found field and index sequence.
				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					if name == "" {
						name = sf.Name
					}

					nameRes = append(nameRes, name)
					indexRes = append(indexRes, index)
					omitemptyRes = append(omitemptyRes, opts.Contains("omitempty"))
					exportRes = append(exportRes, export)
					continue
				}

				// Record anonymous struct to explore in next round.
				nextType = append(nextType, ft)
				nextIndex = append(nextIndex, index)
			}
		}
	}
	return omitemptyRes, indexRes, nameRes, exportRes
}

//go:linkname tcStringBytes
func tcStringBytes(s []byte) (res []byte) {
	return appendString(nil, s, true)
}
