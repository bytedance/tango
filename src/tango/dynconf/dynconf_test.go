// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dynconf

import (
	"fmt"
	"internal/testenv"
	"io/ioutil"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	httpurl string
)

func TestPprofDynconf(t *testing.T) {
	testenv.MustHaveGoBuild(t)

	tmpdir := t.TempDir()
	var port int = 8000 + rand.Int()%1024
	binpath := path.Join(tmpdir, "pprof_dynconf")

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

	t.Run("TestList", testList)
	t.Run("TestGetAndCheck", testGetAndCheck)
	t.Run("TestGetOptmode", testGetOptmode)
	t.Run("TestMaxHeap", testMaxHeap)
	t.Run("TestSet", testSet)
	t.Run("TestTrySetROConfig", testTrySetROConfg)
	t.Run("TestIntOverflow", testIntOverflow)
	t.Run("TestSetGetEquals", testSetGetEquals)
	t.Run("TestRemoteStableness", testRemoteStableness)
}

// retrieve the default expected values of dynconf
func getExpectedCfgs(local bool) map[string]any {
	expectedCfgs := map[string]any{
		"runtime.gab":              int32(0),
		"runtime.gabUsedBytes":     uintptr(0),
		"runtime.gabMemoryPercent": int32(20),
		"runtime.gabEnabled":       false,
		"runtime.gctrace":          int32(0),
		"runtime.gcPercent":        int32(100),
		"runtime.asyncpreemptoff":  int32(0),
		"runtime.madvdontneed":     int32(0),
		"runtime.harddecommit":     int32(0),
		"runtime.maxHeapBytes":     int64(math.MaxInt64),
	}

	if runtime.GOOS == "linux" {
		expectedCfgs["runtime.madvdontneed"] = int32(1)
	}
	return expectedCfgs
}

// check stableness of the remote list
func testRemoteStableness(t *testing.T) {
	// fetch remote configs and generate line-to-index mapping
	// assuming no configs changed during verifying
	fetchRemote := func() map[string]int {
		strres, err := postCmd("list", "")
		if err != nil {
			t.Fatalf("Failed to postCmd 'list', error=%v\n", err)
		}
		indices := make(map[string]int)
		for idx, line := range strings.Split(strres, "\n") {
			indices[line] = idx
		}
		return indices
	}

	// the first one as baseline
	m1 := fetchRemote()
	for i := 0; i < 16; i++ {
		m2 := fetchRemote()
		if !reflect.DeepEqual(m1, m2) {
			t.Fatalf("Unstable results!")
		}
	}
}

func testList(t *testing.T) {
	strres, err := postCmd("list", "")
	if err != nil {
		t.Fatalf("Failed to postCmd 'list', error=%v\n", err)
	}
	expectedStrings := []string{
		"config(ro): runtime.gab=0",
		"config(ro): runtime.gabUsedBytes=0",
		"config(rw): runtime.gabMemoryPercent=20",
		"config(ro): runtime.gabEnabled=false",
		"config(rw): runtime.gctrace=0",
		"config(ro): runtime.buildVersion=",
		"config(ro): runtime.tangoVersion=bm.1.",
		"config(ro): runtime.gcPercent=100",
		"config(ro): runtime.optMode=default",
		"config(ro): runtime.asyncpreemptoff=",
		"config(ro): runtime.madvdontneed=",
		"config(ro): runtime.harddecommit=",
		"config(ro): runtime.maxHeapBytes=",
		"task: runtime.GC",
	}
	if !strings.HasPrefix(runtime.BVersion(), "devel ") {
		expectedStrings = append(expectedStrings, "config(ro): runtime.buildVersion=go1.")
		expectedStrings = append(expectedStrings, "config(ro): runtime.tangoVersion=bm.1.")
	}
	shouldContains(strres, expectedStrings, t)
}

func testGetAndCheck(t *testing.T) {
	for key, val := range getExpectedCfgs(false) {
		strres, err := postCmd("get", key)
		expected := fmt.Sprintf("%s=%v", key, val)
		if err != nil || !strings.Contains(strres, expected) {
			t.Fatalf("Failed to retrieve 'get' output, err=%v, resp=%s, want=%s", err, strres, expected)
		}
	}
}

func testGetOptmode(t *testing.T) {
	testDynConf(t, "get", "runtime.optMode", "runtime.optMode=default")
}

func testMaxHeap(t *testing.T) {
	testDynConf(t, "get", "runtime.maxHeapBytes", fmt.Sprintf("runtime.maxHeapBytes=%d", math.MaxInt64))
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

func testDynConf(t *testing.T, cmd, arg, contains string) {
	strres, err := postCmd(cmd, arg)
	if err != nil || !strings.Contains(strres, contains) {
		t.Fatalf("Failed to retrieve '%s' output, err=%v, resp=%s, want=%s", cmd, err, strres, contains)
	}
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

// Test the dynconf facilities from within current process
func TestLocalGet(t *testing.T) {
	if len(allConfigs) <= 0 {
		t.Fatal("Local dynconf Map is empty")
	}

	dumpValue := func(v any) string {
		typ := reflect.TypeOf(v)
		return fmt.Sprintf("%v (type %s)", v, typ.String())
	}
	for key, val := range getExpectedCfgs(true) {
		if cfg := getcfg(key); cfg != nil {
			v := cfg.v.Interface()
			if v != val {
				t.Fatalf("Dynconf key %s value=%s in local map, but wanted value=%s", key, dumpValue(v), dumpValue(val))
			}
		} else {
			t.Fatalf("Dynconf key %s does exist in local map", key)
		}
	}
}

func TestLocalReadAfterChange(t *testing.T) {
	cfg := getcfg("runtime.gctrace")
	s1 := cfg.String()
	e := cfg.setStringValue("1")
	if e != nil {
		t.Fatal("Failed to set runtime.gctrace")
	}
	defer cfg.setStringValue("0")
	s2 := cfg.String()
	if s1 == s2 {
		t.Fatal("Failed to update config")
	}
	s3 := cfg.String()
	if s2 != s3 {
		t.Fatal("should be same")
	}
}
