// Copyright 2018 The Go Authors. All rights reserved.
// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Portions adapted from Go src/net/http/pprof/pprof_test.go.
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2021-12-14.

package pprof

import (
	"bytes"
	"encoding/json"
	"internal/profile"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Copied from runtime/pprof/proto_tango.go
// Don't reuse the code in runtime/pprof/proto_tango.go.
// Uses hard-coded string instead to make sure we generate the correct file.
// Need to update once the format is changed.
const (
	tangoInfoTag     = "TANGO_INFO_JSON" // begins with it
	tangoInfoVersion = "1.0"             // tango info version
)

// Copied from runtime/pprof/proto_tango.go
// Need to update once the format is changed.
var tangoTestKv = map[string]string{
	"TI_VERSION": tangoInfoVersion,
	"BVERSION":   runtime.BVersion(),
	"GOROOT":     runtime.GOROOT(),
	"GOMAXPROCS": strconv.Itoa(runtime.GOMAXPROCS(-1)),
}

// Copied from runtime/pprof/proto_tango.go
// Need to update once the format is changed.
// Envs we'll check and dump.
var tangoTestEnvs = []string{
	"GOGC",
	"GODEBUG",
	"MY_CPU_LIMIT",
	"MY_CPU_REQUEST",
}

type TangoTestConfig struct {
	path               string
	handler            http.HandlerFunc
	statusCode         int
	contentType        string
	contentDisposition string
	kv                 map[string]string
	env                map[string]string
}

func TestHandlersTangoInfo(t *testing.T) {
	// negative testing
	testCases := []TangoTestConfig{
		{"/debug/pprof/profile?seconds=1&tangoinfo=false", Profile, http.StatusOK, "application/octet-stream", `attachment; filename="profile"`, nil, nil},
		{"/debug/pprof/profile?seconds=1&tangoinfo=xxxx", Profile, http.StatusOK, "application/octet-stream", `attachment; filename="profile"`, nil, nil},
		{"/debug/pprof/profile?seconds=1&tangoinfo", Profile, http.StatusOK, "application/octet-stream", `attachment; filename="profile"`, nil, nil},
		{"/debug/pprof/heap?tangoinfo=false", handler("heap").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="heap"`, nil, nil},
		{"/debug/pprof/heap?tangoinfo=xxxx", handler("heap").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="heap"`, nil, nil},
		{"/debug/pprof/heap?tangoinfo", handler("heap").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="heap"`, nil, nil},
		{"/debug/pprof/goroutine?tangoinfo=false", handler("goroutine").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="goroutine"`, nil, nil},
		{"/debug/pprof/goroutine?tangoinfo=xxxx", handler("goroutine").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="goroutine"`, nil, nil},
		{"/debug/pprof/goroutine?tangoinfo", handler("goroutine").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="goroutine"`, nil, nil},
	}
	for _, tc := range testCases {
		testHandlers(t, tc)
	}

	// positive testing
	testCases = []TangoTestConfig{
		{"/debug/pprof/profile?seconds=1&tangoinfo=true", Profile, http.StatusOK, "application/octet-stream", `attachment; filename="profile"`, tangoTestKv, nil},
		{"/debug/pprof/heap?tangoinfo=true", handler("heap").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="heap"`, tangoTestKv, nil},
		{"/debug/pprof/goroutine?tangoinfo=true", handler("goroutine").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="goroutine"`, tangoTestKv, nil},
		{"/debug/pprof/threadcreate?tangoinfo=true", handler("threadcreate").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="threadcreate"`, tangoTestKv, nil},
		{"/debug/pprof/allocs?tangoinfo=true", handler("allocs").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="allocs"`, tangoTestKv, nil},
		{"/debug/pprof/block?tangoinfo=true", handler("block").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="block"`, tangoTestKv, nil},
		{"/debug/pprof/mutex?tangoinfo=true", handler("mutex").ServeHTTP, http.StatusOK, "application/octet-stream", `attachment; filename="mutex"`, tangoTestKv, nil},
	}
	for _, tc := range testCases {
		oldEnv1 := os.Getenv("GOGC")
		oldEnv2 := os.Getenv("MY_CPU_LIMIT")

		tc.env = map[string]string{"GOGC": "100"}
		os.Setenv("GOGC", tc.env["GOGC"])
		testHandlers(t, tc)

		tc.env = map[string]string{"GOGC": "random''valuexxx,xxx", "MY_CPU_LIMIT": "50"}
		os.Setenv("GOGC", tc.env["GOGC"])
		os.Setenv("MY_CPU_LIMIT", tc.env["MY_CPU_LIMIT"])
		testHandlers(t, tc)

		os.Setenv("GOGC", oldEnv1)
		os.Setenv("MY_CPU_LIMIT", oldEnv2)
		tc.env = nil
		testHandlers(t, tc)
	}
}

func testHandlers(t *testing.T, tc TangoTestConfig) {
	t.Run(tc.path, func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com"+tc.path, nil)
		w := httptest.NewRecorder()
		tc.handler(w, req)

		resp := w.Result()
		if got, want := resp.StatusCode, tc.statusCode; got != want {
			t.Errorf("status code: got %d; want %d", got, want)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Errorf("when reading response body, expected non-nil err; got %v", err)
		}
		if got, want := resp.Header.Get("X-Content-Type-Options"), "nosniff"; got != want {
			t.Errorf("X-Content-Type-Options: got %q; want %q", got, want)
		}
		if got, want := resp.Header.Get("Content-Type"), tc.contentType; got != want {
			t.Errorf("Content-Type: got %q; want %q", got, want)
		}
		if got, want := resp.Header.Get("Content-Disposition"), tc.contentDisposition; got != want {
			t.Errorf("Content-Disposition: got %q; want %q", got, want)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("StatusCode: got %d", resp.StatusCode)
			return
		}

		checkTangoInfo(t, body, tc.kv, tc.env)
	})
}

// Just check whether the profile includes TangoInfo.
// The correctness is checked by Tests in runtime/pprof
func checkTangoInfo(t *testing.T, b []byte, targetkv map[string]string, targetenv map[string]string) {
	p, _ := profile.Parse(bytes.NewBuffer(b))

	found := false
	var cmt string
	for _, cmt = range p.Comments {
		found = strings.Contains(cmt, tangoInfoTag) // looking for the tag
		t.Logf("Comment: %s", cmt)
		if found {
			break
		}
	}

	shouldFound := (targetkv != nil)
	if found != shouldFound {
		t.Errorf("Found TangoInfo:%t, should found TangoInfo:%t", found, shouldFound)
	}
	if found {
		checkMsgJson(t, cmt, targetkv, targetenv)
	}
}

func checkMsgJson(t *testing.T, cmt string, targetkv map[string]string, targetenv map[string]string) {
	// skip "TANGO_INFO_JSON:"
	cmt = cmt[len(tangoInfoTag)+1:]

	var msg map[string]string
	if err := json.Unmarshal([]byte(cmt), &msg); err != nil {
		t.Errorf("Failed to Unmarshal, skip it. %v", err)
		return
	}

	for _, target := range []map[string]string{targetkv, targetenv} {
		for key, val := range target {
			var tangoVal string
			var ok bool
			if tangoVal, ok = msg[key]; !ok {
				t.Fatalf("Didn't find:%s within %s", key, msg)
				continue
			}
			t.Logf("Checking... (%s  %s) -> %s", key, val, tangoVal)
			if tangoVal != val {
				t.Fatalf("Found wrong value:%s !=  %s", tangoVal, val)
			}
		}
	}
}
