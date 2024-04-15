// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pgo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"internal/profile"
	"io"
	"os"
	"strings"
)

// The PGO stack size optimization aims to optimize the CPU usage of runtime.copystack function.
// It will increase the initial stack size alloc for goroutine according to the CPU usage of the
// runtime.copystack function in profile, there by reducing the copystack overhead.
//
// In order to keep the track of stack size used in runtime for continuous optimization, pprof
// will write the initial stack size (stackMin) in tango info json in profile, so that PGO
// can know the previously used initial stack size.
//
// The optimization rules are:
// 1. When the cum CPU usage of runtime.copystack < 0.5%, use an initial stack size of 8KB. It's
//    consistent with the Beast mode.
// 2. When the cum CPU usage of runtime.copystack > 0.5%, rewrite the initial stack size to 32KB.
// 3. When the stack size read in profile is 32KB, continue to use an initial stack size of 32KB
//    to avoid performance regressions.

const (
	pgoDefaultStackSize = "8192"
	pgoBigStackSize     = "32768"
	copyStackFunc       = "runtime.copystack"
	// Use 0.5% as the limit to trigger PGO stack size optimization.
	// This value is analyzed from online services' profile.
	copyStackCumLimit = 0.005
)

func OptStackSize(profileFile string) (string, error) {
	f, err := os.Open(profileFile)
	if err != nil {
		return "", fmt.Errorf("failed to open profile file: %w", err)
	}
	defer f.Close()
	r := bufio.NewReader(f)

	isSerialized, err := IsSerialized(r)
	if err != nil {
		return "", fmt.Errorf("error processing profile header: %w", err)
	}

	if isSerialized {
		return StackSizeFromSerialized(r)
	}

	return StackSizeFromProfile(r)
}

func StackSizeFromProfile(r io.Reader) (string, error) {
	p, err := profile.Parse(r)
	if err != nil {
		return "", fmt.Errorf("failed to Parse profile file: %w", err)
	}

	if len(p.Sample) == 0 {
		// We accept empty profiles, but there is nothing to do.
		return "", nil
	}

	valueIndex := -1
	for i, s := range p.SampleType {
		// Samples count is the raw data collected, and CPU nanoseconds is just
		// a scaled version of it, so either one we can find is fine.
		if (s.Type == "samples" && s.Unit == "count") ||
			(s.Type == "cpu" && s.Unit == "nanoseconds") {
			valueIndex = i
			break
		}
	}

	if valueIndex == -1 {
		return "", fmt.Errorf("failed to find CPU samples count or CPU nanoseconds value-types in profile.")
	}

	return parseStackSizeFromProfile(p, valueIndex), nil
}

func readTangoInfoJson(p *profile.Profile, key string) string {
	const tangoInfoTag = "TANGO_INFO_JSON:"
	for _, cmt := range p.Comments {
		if !strings.Contains(cmt, tangoInfoTag) {
			continue
		}
		// skip "TANGO_INFO_JSON:"
		cmt = cmt[len(tangoInfoTag):]

		var msg map[string]string
		if err := json.Unmarshal([]byte(cmt), &msg); err != nil {
			return ""
		}

		return msg[key]
	}

	return ""
}

// funcCumInProfile counts the cumulative usage of a function in profile.
// It counts the cum value in a simple way without building a node graph.
func funcCumInProfile(p *profile.Profile, name string, valueIndex int) float64 {
	var (
		total int64
		cum   int64
	)

sampleLoop:
	for _, sample := range p.Sample {
		total += sample.Value[valueIndex]
		for _, loc := range sample.Location {
			for _, line := range loc.Line {
				if line.Function != nil && line.Function.Name == name {
					cum += sample.Value[valueIndex]
					// Add value to cum only once for each stack frame
					continue sampleLoop
				}
			}
		}
	}

	if total == 0 {
		return 0
	}

	return float64(cum) / float64(total)
}

func parseStackSizeFromProfile(p *profile.Profile, valueIndex int) string {
	oldStackSize := readTangoInfoJson(p, "STACK_MIN")
	if oldStackSize == pgoBigStackSize {
		return pgoBigStackSize
	}

	copyStackCum := funcCumInProfile(p, copyStackFunc, valueIndex)
	if copyStackCum > copyStackCumLimit {
		return pgoBigStackSize
	}

	return pgoDefaultStackSize
}

func StackSizeFromSerialized(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("error reading preprocessed profile: %w", err)
		}
		return "", fmt.Errorf("preprocessed profile missing header")
	}
	if gotHdr := scanner.Text() + "\n"; gotHdr != tangoSerializationHeader {
		return "", fmt.Errorf("preprocessed profile malformed header; got %q want %q", gotHdr, tangoSerializationHeader)
	}

	for scanner.Scan() {
		readStr := scanner.Text()
		if !strings.HasPrefix(readStr, tangoInfoHeader) {
			continue
		}
		tangoInfo := make(map[string]string)
		err := json.Unmarshal([]byte(strings.TrimPrefix(readStr, tangoInfoHeader)), &tangoInfo)
		if err != nil {
			return "", fmt.Errorf("preprocessed profile error parsing tanfo info: %w", err)
		}

		return tangoInfo["StackSize"], nil
	}

	return "", nil
}
