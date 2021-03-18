// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dynconf_test

import (
	"fmt"
	"internal/testenv"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	httpurl string
)

func TestDynconf(t *testing.T) {
	testenv.MustHaveGoBuild(t)

	tmpdir := fmt.Sprintf("%s/dynconf_test_%d", os.TempDir(), time.Now().UnixNano())
	err := os.Mkdir(tmpdir, 0755)
	if err != nil {
		t.Fatalf("Failed to create tmpdir")
	}
	defer os.RemoveAll(tmpdir)
	var port int = 8000 + rand.Int()%1024

	binpath := fmt.Sprintf("%s/test1", tmpdir)

	cmd := exec.Command(testenv.GoToolPath(t), "build", "-o", binpath, "testdata/svr.go")
	if cmd.Run() != nil {
		t.Fatalf("Failed to compile")
	}

	// Start the server
	go func() {
		cmd := exec.Command(binpath, "-p", fmt.Sprintf("%d", port))
		if cmd.Run() != nil {
			t.Errorf("Failed to start the server.")
		}
	}()

	// wait for the http server to start
	time.Sleep(3 * time.Second)
	defer func() {
		http.Get(fmt.Sprintf("http://127.0.0.1:%d/debug/kill", port))
	}()

	httpurl = fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/dynconf", port)
	t.Logf("httpurl=%s", httpurl)

	testList(t)
	testGet(t)
	testGetGcPercent(t)
	testSet(t)
	testTrySetROConfg(t)
	testIntOverflow(t)
	testSetGetEquals(t)
}

func testList(t *testing.T) {
	strres, err := postCmd("list", "")
	if err != nil {
		t.Fatalf("Failed to postCmd 'list', error=%v\n", err)
	}
	expectedStrings := []string{
		"config(rw): runtime.gab=",
		"config(rw): runtime.gctrace=",
		"config(ro): runtime.buildVersion=",
		"config(ro): runtime.tangoVersion=bm.1.",
		"config(ro): runtime.gcPercent=",
		"task: runtime.GC",
	}
	if !strings.HasPrefix(runtime.BVersion(), "devel ") {
		expectedStrings = append(expectedStrings, "config(ro): runtime.buildVersion=go1.")
		expectedStrings = append(expectedStrings, "config(ro): runtime.tangoVersion=bm.1.")
	}
	shouldContains(strres, expectedStrings, t)
}

func testGet(t *testing.T) {
	strres, err := postCmd("get", "runtime.gctrace")
	if err != nil || !strings.Contains(strres, "runtime.gctrace=") {
		t.Fatalf("Failed to retrieve 'get' output, err=%v, resp=%s", err, strres)
	}
}

func testGetGcPercent(t *testing.T) {
	strres, err := postCmd("get", "runtime.gcPercent")
	if err != nil || !strings.Contains(strres, "runtime.gcPercent=100") {
		t.Fatalf("Failed to retrieve 'get' output, err=%v, resp=%s", err, strres)
	}
}

func testSet(t *testing.T) {
	key := "runtime.gctrace"
	iv := getInt64Value(t, key)

	// do set
	resp, err := http.PostForm(httpurl, url.Values{
		"cmd":  []string{"set"},
		"args": []string{fmt.Sprintf("%s=%d", key, (1 - iv))},
	})
	if err != nil {
		t.Fatalf("Failed to post cmd: 'set'")
	}
	res, err := ioutil.ReadAll(resp.Body)
	strres := strings.TrimSpace(string(res))
	if err != nil || strres != fmt.Sprintf("Successful: %s=%d", key, (1-iv)) {
		t.Fatalf("Failed to read resp body, err=%v, resp=%s", err, strres)
	}

	// restore original value at the end
	defer func() {
		http.PostForm(httpurl, url.Values{
			"cmd":  []string{"set"},
			"args": []string{fmt.Sprintf("%s=%d", key, iv)},
		})
		t.Logf("Config restored: %s=%d", key, iv)
	}()

	iv2 := getInt64Value(t, key)
	if iv2 != (1 - iv) {
		t.Fatalf("Expecting %d, but got %d\n", (1 - iv), iv2)
	}
}

func testTrySetROConfg(t *testing.T) {
	s, e := postCmd("set", "runtime.buildVersion=1234")
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s,
		[]string{
			"Request failed",
			"Failed: config runtime.buildVersion is readonly",
		}, t)
}

func testIntOverflow(t *testing.T) {
	// 17523466567680=0xFF000000000 which overflows int32
	s, e := postCmd("set", "runtime.gctrace=17523466567680")
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s,
		[]string{
			"Request failed",
			"Failed: int overflow, skip setting runtime.gctrace=17523466567680",
		}, t)
}

func testSetGetEquals(t *testing.T) {
	key := "runtime.gctrace"
	iv := int64(rand.Int() % 128)
	s, e := postCmd("set", fmt.Sprintf("%s=%d", key, iv))
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s, []string{fmt.Sprintf("Successful: %s=%d", key, iv)}, t)

	ig := getInt64Value(t, key)
	if ig != iv {
		t.Fatalf("get: %s=%d, but expecting %d", key, ig, iv)
	}
}

// utility functions

func postCmd(cmd, arg string) (string, error) {
	r, e := http.PostForm(httpurl, url.Values{
		"cmd":  []string{cmd},
		"args": []string{arg},
	})
	if e != nil {
		return "", e
	}
	defer r.Body.Close()
	b, e := ioutil.ReadAll(r.Body)
	if e != nil {
		return "", e
	}
	return string(b), nil
}

func getInt64Value(t *testing.T, k string) int64 {
	s, err := postCmd("get", k)
	if err != nil {
		t.Fatalf("Failed to postCmd get, error=%v\n", err)
	}
	s = strings.TrimSpace(strings.Split(s, "=")[1])
	iv, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("Failed to parse: %s", s)
	}
	return iv
}

func shouldContains(s string, keys []string, t *testing.T) {
	for _, k := range keys {
		if !strings.Contains(s, k) {
			t.Fatalf("keyword \"%s\" not found in \"%s\"", k, s)
		}
	}
}
