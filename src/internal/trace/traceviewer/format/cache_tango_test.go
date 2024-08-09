// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Tests continue and break.

package format

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"internal/testenv"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func ctExecCmd(dir, env, name string, arg ...string) (string, string, error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if env != "" {
		cmd.Env = append(os.Environ(), strings.Split(env, ",")...)
	}
	if err := cmd.Run(); err != nil {
		return "", stderr.String(), err
	}

	return stdout.String(), stderr.String(), nil
}

func ctWriteFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func ctDiffMap(sm1, sm2 map[string]*Event, t *testing.T) {
	for k1 := range sm1 {
		if _, ok := sm2[k1]; !ok {
			continue
		}
		sm2[k1] = nil
	}
	for _, v2 := range sm2 {
		if v2 != nil {
			t.Fatalf("some of the constructed caches were not recorded %v.", v2)
		}
	}
}

// ctCleanCache retains the pkg cache and vet cache in the 'cache' file in the 'goCache' directory, and delete the rest.
func ctCleanCache(goCache string, cache string, t *testing.T) {
	sortMap := ctGetSortMap(cache, t)
	fl := ctGetFiles(goCache)
	var cacheSize float64
	var allCacheSize float64
	for _, f := range fl {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		allCacheSize += float64(fi.Size())
		if _, ok := sortMap[f]; ok {
			cacheSize += float64(fi.Size())
			continue
		}
		err = os.Remove(f)
		if err != nil {
			t.Fatal(err)
		}
	}

	rate := cacheSize / allCacheSize
	if rate < 0.98 || rate > 1.0 {
		t.Fatalf("Cache size is abnormal, %f", rate)
	}
}

func ctCheckArgs(cache string, t *testing.T, expectEnv string, expectedArgs []string) {
	b, err := ctUnGzip(cache)
	if err != nil {
		t.Fatal(err)
	}

	res := make([]Event, 0)
	err = json.Unmarshal(b, &res)
	if err != nil {
		t.Fatal(err)
	}

	find := false
	for _, ev := range res {
		if ev.Category != "compiler-info" || ev.Arg == nil {
			continue
		}
		find = true
		arg := ev.Arg.(map[string]interface{})
		args := arg["Args"].([]interface{})
		if len(args) != 3 {
			t.Fatalf("arg %d count mismatch", len(args))
		}
		argMap := ctConvertMap(args)
		for _, a := range expectedArgs {
			ctCheckExpected(argMap, t, a)
		}
		envs := arg["Envs"].([]interface{})
		envMap := ctConvertMap(envs)
		for _, env := range strings.Split(expectEnv, ",") {
			ctCheckExpected(envMap, t, env)
		}
	}
	if !find {
		t.Fatal("compiler info not found in trace file")
	}
}

func ctConvertMap(envs []interface{}) map[string]struct{} {
	envMap := make(map[string]struct{})
	for _, e := range envs {
		envMap[e.(string)] = struct{}{}
	}
	return envMap
}

func ctCheckExpected(envs map[string]struct{}, t *testing.T, expected string) {
	if _, ok := envs[expected]; !ok {
		t.Fatalf("expected %s mismatch", expected)
	}
}

func ctUnGzip(cache string) ([]byte, error) {
	f, err := os.Open(cache)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gzipReader, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	return io.ReadAll(gzipReader)
}

func ctGetCacheFile(cache string, t *testing.T) string {
	files := ctGetFiles(cache)
	for _, file := range files {
		if !strings.HasSuffix(file, ".gz") {
			continue
		}
		newPath := path.Join(t.TempDir(), path.Base(file))
		err := os.Rename(file, newPath)
		if err != nil {
			t.Fatal(err)
		}
		return newPath
	}
	t.Fatal("can't find cache file")
	return ""
}

func ctGetSortMap(cache string, t *testing.T) map[string]*Event {
	b, err := ctUnGzip(cache)
	if err != nil {
		t.Fatal(err)
	}

	res := make([]Event, 0)
	err = json.Unmarshal(b, &res)
	if err != nil {
		t.Fatal(err)
	}

	sortMap := make(map[string]*Event)
	for _, e := range res {
		if !strings.Contains(e.Category, "cache") || e.Category == "hit-cache-rate" ||
			e.Arg == nil {
			continue
		}
		m := e.Arg.(map[string]any)
		fls := m["CacheFile"].([]interface{})
		if len(fls) != 2 {
			continue
		}
		id := fls[0].(string)
		file := fls[1].(string)
		ce := e
		sortMap[id] = &ce
		sortMap[file] = &ce
		fi, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if m["CacheSize"].(float64) != float64(fi.Size()) {
			t.Fatal("Cache size mismatch")
		}
	}
	return sortMap
}

func ctGetFiles(folder string) (filesList []string) {
	files, _ := os.ReadDir(folder)
	for _, file := range files {
		if file.IsDir() {
			filesList = append(filesList, ctGetFiles(filepath.Join(folder, file.Name()))...)
		} else {
			filesList = append(filesList, filepath.Join(folder, file.Name()))
		}
	}
	return
}

func ctPrepareSource(t *testing.T, dir, fuzz string) error {
	buildSource := `package main

import (
  "github.com/bytedance/gopkg/util/logger"
  "context"
)

func main() {
  for i := 0; i < 1000; i++ {
    logger.CtxWarnf(context.Background(), "log information")
  }
}
`
	goModFile := `module testproject

go 1.20

require github.com/bytedance/gopkg %s

`
	if err := ctWriteFile(path.Join(dir, "logoptimization.go"), buildSource+fuzz); err != nil {
		t.Fatal(err)
	}
	testSource := `package main_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/bytedance/gopkg/util/logger"
)

func BenchmarkLogsTango(b *testing.B) {
	runtime.MemProfileRate = 1
	for i := 0; i < b.N; i++ {
		logger.CtxWarnf(context.Background(), "log information")
	}
}
`
	if err := ctWriteFile(path.Join(dir, "logopt_test.go"), testSource); err != nil {
		t.Fatal(err)
	}
	goMod := path.Join(dir, "go.mod")
	err := ctWriteFile(goMod, fmt.Sprintf(goModFile, "v0.1.4"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ctExecCmd(dir, "", testenv.GoToolPath(t), "mod", "tidy")
	if err != nil {
		t.Skip("TestLogOptimization() is skipped due to the network issue.")
	}
	return err
}

func ctCheckFlows(cache string, t *testing.T) {
	b, err := ctUnGzip(cache)
	if err != nil {
		t.Fatal(err)
	}

	res := make([]Event, 0)
	err = json.Unmarshal(b, &res)
	if err != nil {
		t.Fatal(err)
	}

	flowMap := make(map[string]struct{})
	for _, ev := range res {
		if !strings.Contains(ev.Name, "Executing action") {
			continue
		}
		flowEvents := strings.Split(ev.Name, " -> ")
		// Some events with duplicate names have been newly added to the event list,
		// such as "Executing action (collect cgo)"; here, only the package building process is verified.
		if !strings.Contains(ev.Name, "build") {
			continue
		}
		if ev.Phase == "E" {
			flowMap[flowEvents[0]] = struct{}{}
		}
		if ev.Category == "flow" {
			if _, ok := flowMap[flowEvents[0]]; ok {
				t.Fatal("Expected the flow events of the corresponding packages are all before their end event.")
			}
		}
	}
}

func TestCacheTrace(t *testing.T) {
	dir := t.TempDir()
	err := ctPrepareSource(t, dir, "")
	goCache := t.TempDir()
	env := "GOCACHE=" + goCache + ",TANGO_DEBUG_TRACE=" + dir
	args := []string{"build", "-o", "./tmp/logoptimization"}
	// Use a clean gocache directory for testing.
	_, stderr, err := ctExecCmd(dir, env, testenv.GoToolPath(t), args...)
	if err != nil {
		t.Fatal(stderr)
	}
	cacheFile := ctGetCacheFile(dir, t)
	ctCheckFlows(cacheFile, t)
	sm1 := ctGetSortMap(cacheFile, t)
	start := time.Now()
	_, _, err = ctExecCmd(dir, env, testenv.GoToolPath(t), args...)
	if err != nil {
		t.Fatal(err)
	}
	cacheFile = ctGetCacheFile(dir, t)
	diff1 := time.Since(start).Nanoseconds()
	sm2 := ctGetSortMap(cacheFile, t)
	ctDiffMap(sm1, sm2, t)
	ctCheckArgs(cacheFile, t, env, args)
	ctCleanCache(goCache, cacheFile, t)
	start = time.Now()
	_, _, err = ctExecCmd(dir, env, testenv.GoToolPath(t), args...)
	if err != nil {
		t.Fatal(err)
	}
	cacheFile = ctGetCacheFile(dir, t)
	diff2 := time.Since(start).Nanoseconds()
	// Some caches will be rebuilt, but it won't take too much time.
	if float64(diff2)/float64(diff1) > 3 {
		t.Fatalf("The compilation time for a complete cache hit is %d, "+
			"and the compilation time for a hit of both the package and vet caches is %d", diff1, diff2)
	}
}

func TestEnvAndCmd(t *testing.T) {
	dir := t.TempDir()
	err := ctPrepareSource(t, dir, "")
	traceDir := path.Join(dir, "dir_no_exist")
	env := "TANGO_DEBUG_TRACE=" + traceDir
	defaultFile := "tango_debug_trace_[0-9]+.gz"
	noExist := "no exist"
	testArgs := [][]string{
		{"build", defaultFile},
		{"test -o ./tmp/logoptimization", defaultFile},
		{"test", defaultFile},
		{"vet", noExist},
		{"build -debug-trace dir_no_exist/trace.json", "trace.gz"},
		{"test -debug-trace dir_no_exist/trace.json", "trace.gz"},
	}
	for _, args := range testArgs {
		_, _, err = ctExecCmd(dir, env, testenv.GoToolPath(t), strings.Split(args[0], " ")...)
		if err != nil {
			t.Fatal(err)
		}
		files := ctGetFiles(traceDir)
		if args[1] == noExist && len(files) == 0 {
			continue
		}
		if len(files) != 2 {
			t.Fatal("expected 2 file in traceDir")
		}
		// os.ReadDir will sort and ensure that.gz is in the first position.
		fi, err := os.Stat(files[0])
		if err != nil {
			t.Fatal(err)
		}
		matched, err := regexp.MatchString(args[1], fi.Name())
		if err != nil || !matched {
			t.Fatalf("expected file name to match %s", args[1])
		}
		if err := os.RemoveAll(traceDir); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompilerErrorInfo(t *testing.T) {
	dir := t.TempDir()
	err := ctPrepareSource(t, dir, "\nvar")
	goCache := t.TempDir()
	env := "GOCACHE=" + goCache + ",TANGO_DEBUG_TRACE=" + dir
	args := []string{"build", "-o", "./tmp/logoptimization"}
	// Use a clean gocache directory for testing.
	_, _, err = ctExecCmd(dir, env, testenv.GoToolPath(t), args...)
	if err == nil {
		t.Fatal("expected compilation error")
	}
	cacheFile := ctGetCacheFile(dir, t)
	ctCheckCompilerError(cacheFile, t)
}

func ctCheckCompilerError(cache string, t *testing.T) {
	b, err := ctUnGzip(cache)
	if err != nil {
		t.Fatal(err)
	}

	res := make([]Event, 0)
	err = json.Unmarshal(b, &res)
	if err != nil {
		t.Fatal(err)
	}

	find := false
	for _, ev := range res {
		if ev.Category != "compiler-error" || ev.Arg == nil {
			continue
		}
		find = true
		args := ev.Arg.(map[string]interface{})
		if len(args) != 2 {
			t.Fatalf("arg %d count mismatch", len(args))
		}
		if errCode, ok := args["Err"].(string); !ok || !strings.Contains(errCode,
			"compile: exit status 2") {
			t.Fatalf("arg %v unexpected", ev.Arg)
		}
		if errCode, ok := args["PositionAndContent"].(string); !ok || !strings.Contains(errCode,
			"logoptimization.go:14:4: syntax error: unexpected EOF, expected name") {
			t.Fatalf("arg %v unexpected", ev.Arg)
		}
	}
	if !find {
		t.Fatal("compiler-error not found")
	}
}
