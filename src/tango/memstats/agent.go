// Copyright 2010 The Go Authors. All rights reserved.
// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Portions adapted from Go src/net/http/pprof/pprof.go.
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2021-05-25.

package memstats

import (
	"fmt"
	"internal/godebug"
	"net/http"
	"strconv"
)

func init() {
	prefix := ""
	if godebug.New("httpmuxgo121").Value() != "1" {
		prefix = "GET "
	}
	http.HandleFunc(prefix+"/debug/pprof/memstats", memStats)
}

func serveError(w http.ResponseWriter, status int, txt string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Go-Pprof", "1")
	w.Header().Del("Content-Disposition")
	w.WriteHeader(status)
	fmt.Fprintln(w, txt)
}

func durationExceedsWriteTimeout(r *http.Request, seconds float64) bool {
	srv, ok := r.Context().Value(http.ServerContextKey).(*http.Server)
	return ok && srv.WriteTimeout != 0 && seconds >= srv.WriteTimeout.Seconds()
}

func memStats(w http.ResponseWriter, r *http.Request) {
	// text-format is an option to support command-line environment later
	html := true

	w.Header().Set("X-Content-Type-Options", "nosniff")
	if html {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}

	sec, err := strconv.ParseInt(r.FormValue("seconds"), 10, 64)
	if sec <= 0 || err != nil {
		sec = 0
	} else if durationExceedsWriteTimeout(r, float64(sec)) {
		serveError(w, http.StatusBadRequest, "duration exceeds server's WriteTimeout")
		return
	}

	// sort the stats. of small-size group
	sort, _ := strconv.Atoi(r.FormValue("sort"))
	res := collectMemStats(r, sec, sort != 0)
	if html {
		htmlWrite(w, r, sec, res)
	}
}
