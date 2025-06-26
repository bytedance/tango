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
	clear := buildAndRunDynconfServer(t, nil, nil)
	defer clear()
	t.Run("TestList", testList)
	t.Run("TestGetAndCheck", testGetAndCheck)
	t.Run("TestGetOptmode", testGetOptmode)
	t.Run("TestMaxHeap", testMaxHeap)
	t.Run("TestGetLargeAllocTrace", testGetLargeAllocTrace)
	t.Run("TestSet", testSet)
	t.Run("TestTrySetROConfig", testTrySetROConfg)
	t.Run("TestIntOverflow", testIntOverflow)
	t.Run("TestSetGetEquals", testSetGetEquals)
	t.Run("TestRemoteStableness", testRemoteStableness)
	t.Run("TestMemoryLimit", testMemoryLimit)
	t.Run("TestLargeAllocTrace", testLargeAllocTrace)
}

func TestPprofDynconfLocal(t *testing.T) {
	var port int = 10000 + rand.Int()%1024
	// wait for the http server to start
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	go http.ListenAndServe(addr, nil)
	httpurl = fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/dynconf", port)
	t.Logf("httpurl=%s", httpurl)
	// wait for the http server to start
	time.Sleep(3 * time.Second)
	t.Run("TestRegister", testRegister)
	t.Run("TestRegisterError", testRegisterError)
}

func TestPprofDynconfWithDefaultEnv(t *testing.T) {
	clear := buildAndRunDynconfServer(t, nil, []string{
		"GODEBUG=gctrace=1",
		"GOGC=off",
		"GOMEMLIMIT=4294967296",
	})
	defer clear()
	expectedStrings := []string{
		"config(rw): runtime.gctrace=1",
		"config(ro): runtime.gcPercent=-1",
		"config(ro): runtime.memoryLimit=4294967296",
	}
	listAndCheck(t, expectedStrings, "")
	// test gc throughput EMA read.
	if strres, err := postCmd("get", "runtime.gcadaptiveThrptEMA"); err != nil || !strings.Contains(strres, "runtime.gcadaptiveThrptEMA=") {
		t.Fatalf("Failed to get runtime.gcadaptiveThrptEMA, error=%v, response=%s", err, strres)
	} else {
		// the value should be a float number, which is the GC throughput EMA.
		strres = strings.TrimSpace(strings.Split(strres, "=")[1])
		// check strres can be parsed as float64 and is non-negative.
		if f, err := strconv.ParseFloat(strres, 64); err != nil || f < 0 || f > 100.1 {
			t.Fatalf("Invalid runtime.gcadaptiveThrptEMA value, error=%v, value=%s", err, strres)
		}
	}
}

func TestPprofDynconfWithGCAdaptiveEnv(t *testing.T) {
	clear := buildAndRunDynconfServer(t, nil, []string{
		"GODEBUG=gctrace=1,gcadaptive=1,gcadaptivetrace=1",
		"GOGC=off",
		"GOMEMLIMIT=4294967296",
	})
	defer clear()
	expectedStrings := []string{
		"config(rw): runtime.gctrace=1",
		"config(rw): runtime.gcadaptive=1",
		"config(rw): runtime.gcadaptivetrace=1",
		"config(ro): runtime.gcPercent=100",
		"config(ro): runtime.memoryLimit=4294967296",
	}
	listAndCheck(t, expectedStrings, "")
}

// retrieve the default expected values of dynconf
func getExpectedCfgs(local bool) map[string]any {
	expectedCfgs := map[string]any{
		"runtime.gab":              int32(0),
		"runtime.gabUsedBytes":     uintptr(0),
		"runtime.gabThreshold":     uint64(0),
		"runtime.gabMemoryPercent": int32(20),
		"runtime.gabEnabled":       false,
		"runtime.gctrace":          int32(0),
		"runtime.gcPercent":        int32(100),
		"runtime.asyncpreemptoff":  int32(0),
		"runtime.madvdontneed":     int32(0),
		"runtime.harddecommit":     int32(0),
		"runtime.maxHeapBytes":     int64(math.MaxInt64),
		"runtime.largeAllocTrace":  int32(0),
		"runtime.optMode":          "default",
		"runtime.pgoSource":        "none",
		"runtime.memoryLimit":      int64(math.MaxInt64),
		"runtime.gcadaptive":       int32(0),
		"runtime.gcadaptivetrace":  int32(0),
		"runtime.moduledataOpt":    int32(0),
		"runtime.heapHugePage":     int32(0),
	}

	if local {
		if testenv.IsBeastModeEnabled() {
			expectedCfgs["runtime.optMode"] = "beast"
		} else {
			expectedCfgs["runtime.optMode"] = "default"
		}
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
	if testenv.IsLLVMEnabled() {
		t.Skip("Temporarily skip this test case in LLVM mode")
	}
	expectedStrings := []string{
		"config(ro): runtime.gab=0",
		"config(ro): runtime.gabUsedBytes=0",
		"config(rw): runtime.gabMemoryPercent=20",
		"config(ro): runtime.gabEnabled=false",
		"config(rw): runtime.gctrace=0",
		"config(rw): runtime.gcadaptive=0",
		"config(rw): runtime.gcadaptivetrace=0",
		"config(rw): runtime.gcadaptiveThrptGoal=",
		"config(rw): runtime.gcadaptiveThrptGoalLower=",
		"config(rw): runtime.gcadaptiveSigmoidKValue=",
		"config(ro): runtime.gcadaptiveThrptEMA=",
		"config(ro): runtime.buildVersion=",
		"config(ro): runtime.tangoVersion=bm.1.",
		"config(ro): runtime.pgoSource=none",
		"config(ro): runtime.gcPercent=100",
		"config(ro): runtime.optMode=default",
		"config(rw): runtime.largeAllocTrace=",
		"config(ro): runtime.asyncpreemptoff=",
		"config(ro): runtime.madvdontneed=",
		"config(ro): runtime.harddecommit=",
		"config(ro): runtime.maxHeapBytes=",
		"config(ro): runtime.gabThreshold=",
		"config(ro): runtime.memoryLimit=",
		"config(ro): runtime.artsOpts=none",
		"config(rw): runtime.moduledataOpt=0",
		"config(ro): runtime.heapHugePage=0",
		"task: runtime.GC",
	}
	listAndCheck(t, expectedStrings, "")
}

func testGetAndCheck(t *testing.T) {
	if testenv.IsLLVMEnabled() {
		t.Skip("Temporarily skip this test case in LLVM mode")
	}
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

func testGetLargeAllocTrace(t *testing.T) {
	testDynConf(t, "get", "runtime.largeAllocTrace", "runtime.largeAllocTrace=0")
}

func testGetArtsOpts(t *testing.T) {
	strres, err := postCmd("get", "runtime.artsOpts")
	if err != nil || !strings.Contains(strres, "runtime.artsOpts=none") {
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

	// 17523466567680=0xFF000000000 which overflows int32
	s, e = postCmd("set", "runtime.gcadaptivetrace=17523466567680")
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s,
		[]string{
			"Request failed",
			"Failed: int overflow, skip setting runtime.gcadaptivetrace=17523466567680",
		}, t)

	s, e = postCmd("set", "runtime.gcadaptive=17523466567680")
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s,
		[]string{
			"Request failed",
			"Failed: int overflow, skip setting runtime.gcadaptive=17523466567680",
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

	key = "runtime.gcadaptivetrace"
	iv = int64(rand.Int() % 128)
	s, e = postCmd("set", fmt.Sprintf("%s=%d", key, iv))
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s, []string{fmt.Sprintf("Successful: %s=%d", key, iv)}, t)

	ig = getInt64Value(t, key)
	if ig != iv {
		t.Fatalf("get: %s=%d, but expecting %d", key, ig, iv)
	}

	key = "runtime.gcadaptive"
	iv = int64(rand.Int() % 128)
	s, e = postCmd("set", fmt.Sprintf("%s=%d", key, iv))
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s, []string{fmt.Sprintf("Successful: %s=%d", key, iv)}, t)

	ig = getInt64Value(t, key)
	if ig != iv {
		t.Fatalf("get: %s=%d, but expecting %d", key, ig, iv)
	}

	key = "runtime.gcadaptiveThrptGoal"
	fiv := float64(rand.Int()%128) / 10.0
	s, e = postCmd("set", fmt.Sprintf("%s=%s", key, strconv.FormatFloat(fiv, 'g', -1, 64)))
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s, []string{fmt.Sprintf("Successful: %s=%v", key, fiv)}, t)

	key = "runtime.gcadaptiveThrptGoalLower"
	fiv = float64(rand.Int()%128) / 10.0
	s, e = postCmd("set", fmt.Sprintf("%s=%s", key, strconv.FormatFloat(fiv, 'g', -1, 64)))
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s, []string{fmt.Sprintf("Successful: %s=%v", key, fiv)}, t)

	key = "runtime.gcadaptiveSigmoidKValue"
	fiv = float64(rand.Int()%128) / 10.0
	s, e = postCmd("set", fmt.Sprintf("%s=%s", key, strconv.FormatFloat(fiv, 'g', -1, 64)))
	if e != nil {
		t.Fatalf("Failed to postCmd set, error=%v\n", e)
	}
	shouldContains(s, []string{fmt.Sprintf("Successful: %s=%v", key, fiv)}, t)

	fig := getFloat64Value(t, key)
	if fig != fiv {
		t.Fatalf("get: %s=%v, but expecting %v", key, fig, fiv)
	}
}

// utility functions

func buildAndRunDynconfServer(t *testing.T, extBuildArgs []string, envs []string) func() {
	testenv.MustHaveGoBuild(t)

	tmpdir := t.TempDir()
	var port int = 8000 + rand.Int()%1024
	binpath := path.Join(tmpdir, "pprof_dynconf")

	args := append([]string{"build", "-o", binpath}, extBuildArgs...)
	cmd := exec.Command(testenv.GoToolPath(t), append(args, "testdata/svr.go")...)
	if cmd.Run() != nil {
		t.Fatalf("Failed to compile")
	}

	// Start the server
	go func() {
		cmd := exec.Command(binpath, "-p", fmt.Sprintf("%d", port))
		cmd.Env = append(cmd.Env, envs...)
		if cmd.Run() != nil {
			t.Errorf("Failed to start the server.")
		}
	}()

	// wait for the http server to start
	time.Sleep(3 * time.Second)

	httpurl = fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/dynconf", port)
	t.Logf("httpurl=%s", httpurl)
	return func() {
		http.Get(fmt.Sprintf("http://127.0.0.1:%d/debug/kill", port))
	}
}

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

func getFloat64Value(t *testing.T, k string) float64 {
	s, err := postCmd("get", k)
	if err != nil {
		t.Fatalf("Failed to postCmd get, error=%v\n", err)
	}
	s = strings.TrimSpace(strings.Split(s, "=")[1])
	iv, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("Failed to parse: %s", s)
	}
	return iv
}

func listAndCheck(t *testing.T, expectedStrings []string, args string) {
	strres, err := postCmd("list", args)
	if err != nil {
		t.Fatalf("Failed to postCmd 'list', error=%v\n", err)
	}

	shouldContains(strres, expectedStrings, t)
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
	if testenv.IsLLVMEnabled() {
		t.Skip("Temporarily skip this test case in LLVM mode")
	}
	// lazyInit will not be executed if no http request is ever sent.
	// Call it manually during local testing to avoid inconsistent PGO results when other tests send requests.
	lazyInit()
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

func TestPprofDynconfPGO(t *testing.T) {
	clear := buildAndRunDynconfServer(t, []string{"-pgo", "testdata/prof"}, nil)
	defer clear()
	t.Run("TestListPGO", testListPGO)
}

func testListPGO(t *testing.T) {
	expectedStrings := []string{
		"config(ro): runtime.pgoSource=prof",
		"task: runtime.GC",
	}
	listAndCheck(t, expectedStrings, "")
}

// test change memory limit by debug.SetMemoryLimit.
func testMemoryLimit(t *testing.T) {
	memLimitUrl := strings.Replace(httpurl, "pprof/dynconf", "setMemoryLimit", -1)
	key := "runtime.memoryLimit"
	// restore original value at the end
	defer func() {
		http.Get(memLimitUrl + "?value=" + strconv.Itoa(math.MaxInt64))
	}()
	// do set
	newValue := int64(1024 * 1024 * 1024)
	_, err := http.Get(memLimitUrl + "?value=" + strconv.FormatInt(newValue, 10))
	if err != nil {
		t.Fatalf("Failed to set memorylimit")
	}
	value := getInt64Value(t, "runtime.memoryLimit")
	if value != newValue {
		t.Fatalf("get: %s=%d, but expecting %d", key, value, newValue)
	}
}

// only for test to clear register dynconf.
func clearRegisterDynconf() {
	registerConfMutex.Lock()
	defer registerConfMutex.Unlock()
	registerConf = nil
}

func testRegister(t *testing.T) {
	defer clearRegisterDynconf()
	err := registerDynconf("Test", "AAA", "true", false)
	if err != nil {
		t.Errorf("registerDynconf unexpected error %v", err)
	}
	expectedStrings := []string{"config(ro): Test.AAA=true"}
	// after successful registration, adding the extern=all or extern=namespace parameter should display the config.
	listAndCheck(t, expectedStrings, "extern=all")
	listAndCheck(t, expectedStrings, "extern=Test")
	err = registerDynconf("Test", "BBB", "false", false)
	if err != nil {
		t.Errorf("registerDynconf unexpected error %v", err)
	}
	listAndCheck(t, []string{"Test.AAA=true", "Test.BBB=false"}, "extern=Test")
	// get should also be able to obtain the registered config.
	cfg := getcfg("Test.AAA")
	expect := "config(ro): Test.AAA=true"
	if cfg.String() != expect {
		t.Errorf("get config details failed, expected %s, got %s", expect, cfg.String())
	}
	// registered configs should also respect read-only and writable attributes.
	err = cfg.setStringValue("true")
	if err == nil {
		t.Errorf("set readonly config unexpect success")
	}
}

func testRegisterError(t *testing.T) {
	defer clearRegisterDynconf()
	// register with the reserved namespace.
	err := registerDynconf(runtimeNameSpace, "AAA", 1, false)
	if err != errReservedNamespace {
		t.Errorf("expected error %v, got %v", errReservedNamespace, err)
	}

	err = registerDynconf(tangoNameSpace, "AAA", 1, false)
	if err != errReservedNamespace {
		t.Errorf("expected error %v, got %v", errReservedNamespace, err)
	}
	// register with incorrect namespace format.
	err = registerDynconf("Test.AAA", "BBB", 1, false)
	if err != errParameterFormat {
		t.Errorf("expected error %v, got %v", errParameterFormat, err)
	}
	// register with incorrect key format.
	err = registerDynconf("Test", "AAA.BBB", 1, false)
	if err != errParameterFormat {
		t.Errorf("expected error %v, got %v", errParameterFormat, err)
	}
	// all registrations above should fail and the list should be empty.
	if dyns := getAllRegisterDynconf(); len(dyns) != 0 {
		t.Errorf("unexpected register success")
	}
	// when the number of namespaces is less than namespaceThreshold, registration should success.
	for i := 0; i < namespaceThreshold; i++ {
		err = registerDynconf(fmt.Sprintf("Test%d", i), "AAA", 1, false)
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
	}
	// when the number of namespaces exceeds namespaceThreshold, registration should fail.
	err = registerDynconf(fmt.Sprintf("Test%d", namespaceThreshold), "AAA", 1, false)
	if err != errNamespaceThreshold {
		t.Errorf("expected error %v, got %v", errNamespaceThreshold, err)
	}
	// when the number of configs per namespace is less than confThresholdPerNamespace, registration should success.
	// Note: for Test0 namespace, one has been successfully registered above,
	// and the number checked here is confThresholdPerNamespace-1
	for i := 0; i < confThresholdPerNamespace-1; i++ {
		err = registerDynconf(fmt.Sprintf("Test%d", 0), fmt.Sprintf("AAA%d", i), 1, false)
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
	}
	// when the number of configs per namespace exceeds confThresholdPerNamespace, registration should fail.
	err = registerDynconf(fmt.Sprintf("Test%d", 0), fmt.Sprintf("AAA%d", confThresholdPerNamespace), 1, false)
	if err != errConfThreshold {
		t.Errorf("expected error %v, got %v", errConfThreshold, err)
	}
}

var memdebugglobal interface{}

func allocMemory(size int64) {
	memdebugglobal = make([]byte, size)
}

func checkLargeAllocTraceOutput(output string) bool {
	lines := strings.Split(output, "\n")
	shouldRecord := false
	allocSize := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "Alloc Record") {
			if shouldRecord {
				return false
			}
			shouldRecord = true
			allocSize = strings.TrimSpace(strings.TrimPrefix(line, "Alloc Record"))
		} else if strings.HasPrefix(line, "Alloc Not Record") {
			if shouldRecord {
				return false
			}
			shouldRecord = false
			allocSize = ""
		} else {
			if strings.HasPrefix(line, "[Memory]") {
				if !shouldRecord {
					return false
				}
				if !strings.Contains(line, "tango/dynconf.allocMemory") || !strings.Contains(line, "bytes="+allocSize) {
					return false
				}
				// check success
				shouldRecord = false
			} else {
				if shouldRecord {
					return false
				}
			}
		}
	}
	return true
}

func testLargeAllocTrace(t *testing.T) {
	key := "runtime.largeAllocTrace"
	iv := getInt64Value(t, key)

	// do set
	var size int64 = 10 * 1024 * 1024
	resp, err := http.PostForm(httpurl, url.Values{
		"cmd":  []string{"set"},
		"args": []string{fmt.Sprintf("%s=%d", key, size)},
	})
	if err != nil {
		t.Fatalf("Failed to post cmd: 'set'")
	}
	res, err := ioutil.ReadAll(resp.Body)
	strres := strings.TrimSpace(string(res))
	if err != nil || strres != fmt.Sprintf("Successful: %s=%d", key, size) {
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
	if iv2 != size {
		t.Fatalf("Expecting %d, but got %d\n", size, iv2)
	}
}

func TestPprofDynconfWithHeapHugePageEnv(t *testing.T) {
	clear := buildAndRunDynconfServer(t, nil, []string{
		"GODEBUG=heaphugepage=1",
	})

	defer clear()

	expectedStrings := []string{
		"config(ro): runtime.heapHugePage=1",
	}

	listAndCheck(t, expectedStrings, "")
}
