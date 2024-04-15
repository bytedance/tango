// Copyright 2024 The Go Authors. All rights reserved.
// Copyright (c) 2024 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2024-04-15.

package pgo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// IsSerialized returns true if r is a serialized Profile.
//
// IsSerialized only peeks at r, so seeking back after calling is not
// necessary.
func IsSerialized(r *bufio.Reader) (bool, error) {
	hdr, err := r.Peek(len(serializationHeader))
	if err == io.EOF {
		// Empty file.
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("error reading profile header: %w", err)
	}

	if string(hdr) == serializationHeader {
		return true, nil
	}

	hdr, err = r.Peek(len(tangoSerializationHeader))
	if err != nil {
		return false, fmt.Errorf("error reading profile header: %w", err)
	}

	return string(hdr) == tangoSerializationHeader, nil
}

// FromSerialized parses a profile from serialization output of Profile.WriteTo.
func FromSerialized(r io.Reader) (*Profile, error) {
	d := emptyProfile()

	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("error reading preprocessed profile: %w", err)
		}
		return nil, fmt.Errorf("preprocessed profile missing header")
	}
	if gotHdr := scanner.Text() + "\n"; gotHdr != serializationHeader && gotHdr != tangoSerializationHeader {
		return nil, fmt.Errorf("preprocessed profile malformed header; got %q want %q or %q", gotHdr, serializationHeader, tangoSerializationHeader)
	}

	for scanner.Scan() {
		readStr := scanner.Text()
		// skip the TANGO INFO: header lines
		if strings.HasPrefix(readStr, tangoInfoHeader) {
			err := json.Unmarshal([]byte(strings.TrimPrefix(readStr, tangoInfoHeader)), &d.TangoInfo)
			if err != nil {
				return nil, fmt.Errorf("preprocessed profile error parsing tango info: %w", err)
			}
			continue
		}

		callerName := readStr

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, fmt.Errorf("error reading preprocessed profile: %w", err)
			}
			return nil, fmt.Errorf("preprocessed profile entry missing callee")
		}
		calleeName := scanner.Text()

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, fmt.Errorf("error reading preprocessed profile: %w", err)
			}
			return nil, fmt.Errorf("preprocessed profile entry missing weight")
		}
		readStr = scanner.Text()

		split := strings.Split(readStr, " ")

		if len(split) != 2 {
			return nil, fmt.Errorf("preprocessed profile entry got %v want 2 fields", split)
		}

		co, err := strconv.Atoi(split[0])
		if err != nil {
			return nil, fmt.Errorf("preprocessed profile error processing call line: %w", err)
		}

		edge := NamedCallEdge{
			CallerName:     callerName,
			CalleeName:     calleeName,
			CallSiteOffset: co,
		}

		weight, err := strconv.ParseInt(split[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("preprocessed profile error processing call weight: %w", err)
		}

		if _, ok := d.NamedEdgeMap.Weight[edge]; ok {
			return nil, fmt.Errorf("preprocessed profile contains duplicate edge %+v", edge)
		}

		d.NamedEdgeMap.ByWeight = append(d.NamedEdgeMap.ByWeight, edge) // N.B. serialization is ordered.
		d.NamedEdgeMap.Weight[edge] += weight
		d.TotalWeight += weight
	}

	return d, nil

}
