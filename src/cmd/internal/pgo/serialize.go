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
)

// Serialization of a Profile allows go tool preprofile to construct the edge
// map only once (rather than once per compile process). The compiler processes
// then parse the pre-processed data directly from the serialized format.
//
// The format of the serialized output is as follows.
//
//      GO PREPROFILE V1
//      caller_name
//      callee_name
//      "call site offset" "call edge weight"
//      ...
//      caller_name
//      callee_name
//      "call site offset" "call edge weight"
//
// Entries are sorted by "call edge weight", from highest to lowest.

const serializationHeader = "GO PREPROFILE V1\n"

// tangoSerializationHeader is the header to distinguish the tango version of
// serialized profile format from the community version.
// tangoInfoHeader is added for tango internal PGO enhancement features.
// It will contain the info parsed from the original profile file,
// and be used for tango PGO enhancement features like PGO stack size.
// The format of the serialized output with tangoSerializationHeader and
// tangoInfoHeader is as follows.
//
//	TANGO PREPROFILE V1
//	TANGO INFO: {"StackSize": "2048"}
//	caller_name
//	callee_name
//	"call site offset" "call edge weight"
const (
	tangoSerializationHeader = "TANGO PREPROFILE V1\n"
	tangoInfoHeader          = "TANGO INFO:"
)

// WriteTo writes a serialized representation of Profile to w.
//
// FromSerialized can parse the format back to Profile.
//
// WriteTo implements io.WriterTo.Write.
func (d *Profile) WriteTo(w io.Writer) (int64, error) {
	bw := bufio.NewWriter(w)

	var (
		written int64
		header  = serializationHeader
	)

	if len(d.TangoInfo) > 0 {
		header = tangoSerializationHeader
	}

	// Header
	n, err := bw.WriteString(header)
	written += int64(n)
	if err != nil {
		return written, err
	}

	// Tango info header
	if len(d.TangoInfo) > 0 {
		tangoInfo, err := json.Marshal(d.TangoInfo)
		if err != nil {
			return written, err
		}

		n, err := bw.WriteString(fmt.Sprintf("%s%s\n", tangoInfoHeader, tangoInfo))
		written += int64(n)
		if err != nil {
			return written, err
		}
	}

	for _, edge := range d.NamedEdgeMap.ByWeight {
		weight := d.NamedEdgeMap.Weight[edge]

		n, err = fmt.Fprintln(bw, edge.CallerName)
		written += int64(n)
		if err != nil {
			return written, err
		}

		n, err = fmt.Fprintln(bw, edge.CalleeName)
		written += int64(n)
		if err != nil {
			return written, err
		}

		n, err = fmt.Fprintf(bw, "%d %d\n", edge.CallSiteOffset, weight)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}

	if err := bw.Flush(); err != nil {
		return written, err
	}

	// No need to serialize TotalWeight, it can be trivially recomputed
	// during parsing.

	return written, nil
}
