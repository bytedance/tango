// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flag_beast

import (
	"bytes"
	"log"
	"sort"
	"strconv"
	"strings"
)

func ParseBeastArguments(beastStr string) (res map[string]int) {
	res = make(map[string]int)
	// parse -beast argument
	beastSeps, idx, beastBytes := ":,", 0, []byte(beastStr)
	for idx < len(beastStr) {
		if strings.Contains(beastSeps, string(beastStr[idx])) {
			idx++
			continue
		}
		start := idx
		idx = bytes.IndexAny(beastBytes[idx:], beastSeps)
		opt := ""
		if idx == -1 {
			opt = string(beastBytes[start:])
			idx = len(beastStr) // Use boundary values to end the loop.
		} else {
			idx += start
			opt = string(beastBytes[start:idx])
			idx++
		}
		i := strings.IndexAny(opt, "=")
		if i < 0 {
			res[opt] = 1
			continue
		}
		val, err := strconv.Atoi(opt[i+1:])
		if err != nil {
			val = 1
		}
		if val < 0 {
			log.Fatalf("illegal option value %d", val)
		}
		res[opt[:i]] = val
	}
	return
}

func mergeBeastSubOption(opts1, opts2 string) string {
	if opts1 == "" {
		return opts2
	}
	if opts2 == "" {
		return opts1
	}
	return opts1 + "," + opts2
}

func GetBeastStr(gcflags []string) string {
	beastStr := ""
	// Refer to the logic of the -d option for processing sub-options,
	// beast will also process all sub-options, so it is necessary to extract the values of all sub-options.
	for i, v := range gcflags {
		if strings.HasPrefix(v, "-beast=") {
			beastStr = mergeBeastSubOption(beastStr, strings.TrimPrefix(v, "-beast="))
		} else if v == "-beast" && i+1 < len(gcflags) {
			beastStr = mergeBeastSubOption(beastStr, gcflags[i+1])
		}
	}

	return beastStr
}

func GetBeastFlag(gcflags []string) string {
	beastFlag := ""
	beastVars := ParseBeastArguments(GetBeastStr(gcflags))
	beastOpts := make([]string, 0, len(beastVars))
	for opt := range beastVars {
		beastOpts = append(beastOpts, opt)
	}
	sort.Strings(beastOpts)
	for _, opt := range beastOpts {
		val := beastVars[opt]
		// To facilitate identification of which beast sub-features have been enabled
		// based on the presence of strings, only the enabled features are shown here.
		if val <= 0 {
			continue
		}
		beastFlag = mergeBeastSubOption(beastFlag, opt+"="+strconv.Itoa(val))
	}
	return beastFlag
}
