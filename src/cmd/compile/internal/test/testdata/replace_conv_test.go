// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"internal/testenv"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
)

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func execCmd(dir, env, name string, arg ...string) (string, string, error) {
	cmd := exec.Command(name, arg...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if env != "" {
		cmd.Env = append(os.Environ(), strings.Split(env, ",")...)
	}
	if err := cmd.Run(); err != nil {
		return stdout.String(), stderr.String(), err
	}
	return stdout.String(), stderr.String(), nil
}

func rcDownloadMod(t *testing.T, dir string) bool {
	netErrors := []string{"timeout", "connect", "certificate", "host"}
tryLoop:
	for tryCount := 0; tryCount < 5; tryCount++ {
		stdout, stderr, err := execCmd(
			dir, "", testenv.GoToolPath(t), "mod", "tidy",
		)
		if err == nil {
			return true
		}
		for _, netError := range netErrors {
			if strings.Contains(stdout, netError) ||
				strings.Contains(stderr, netError) {
				continue tryLoop
			}
		}
		t.Fatal("stderr:\n", stderr, "\nstdout:\n", stdout)
	}
	return false
}

var (
	rcImportMods  = []string{"github.com/mohae/deepcopy", "github.com/jinzhu/copier"}
	rcImports     = []string{"github.com/mohae/deepcopy", "github.com/jinzhu/copier"}
	rcWrapperFunc = []string{"tcDeepCopyWrapper", "tcCopierWrapper"}
	rcFuncs       = []string{"Copy", "CopyWithOption"}
	rcVersions    = []string{"c48cc78d482608239f6c4c92a4", "v0.2.7"} // The first version with the corresponding function.
	rcLocations   = [][]string{{":24:24:", ":30:24:"}, {":26:28:"}}
	rcAllocations = [][]string{{"3", "4"}, {"2"}}
	rcReplaceFunc = [][]string{{"", `func (s s1) DeepCopy() interface{} {
  var res s1
  res.f1 = 3
  res.F2 = "success"
  res.F3 = 6.28
  return res
}`}, {""}}
	rcResults = [][]string{{"{0 aa 3.14}", "{3 success 6.28}"}, {"{1 aa 3.14}"}}
	rcSource  = `package replace_conv_test

import (
  "fmt"
  "runtime"
  "testing"

  deepcopy "%s"
)

type s1 struct {
  f1 int
  F2 string
  F3 float64
}

%s

func BenchmarkReplaceConv(b *testing.B) {
  runtime.MemProfileRate = 1
  from := s1{1, contact("a", "a"), 3.14}
  var res interface{}
  for i := 0; i < b.N; i++ {
    %s
  }
  fmt.Printf("res is %s\n", res)
}

func contact(a string, b string) string {
	return fmt.Sprintf("%%v%%v", a, b)
}
`
	rcGoModFile = `module main

go 1.19

require %s %s

`
)

func getDeepCopyCode(i int) string {
	switch rcImportMods[i] {
	case "github.com/mohae/deepcopy":
		return "res = deepcopy.Copy(from)"
	case "github.com/jinzhu/copier":
		return `res = s1{}
    option := &deepcopy.Option{DeepCopy: true}
    deepcopy.CopyWithOption(&res, from, *option)`
	}
	return ""
}

func TestReplaceConv(t *testing.T) {
	dir := t.TempDir()
	t.Logf("Working directory: %s\n", dir)
	goMod := path.Join(dir, "go.mod")
	p := path.Join(dir, "replace_conv_test.go")

	for i := range rcImportMods {
		err := writeFile(goMod, fmt.Sprintf(rcGoModFile, rcImportMods[i], rcVersions[i]))
		if err != nil {
			t.Fatal(err)
		}
		deepCopy := getDeepCopyCode(i)
		for j := range rcReplaceFunc[i] {
			if err := writeFile(p, fmt.Sprintf(rcSource, rcImports[i], rcReplaceFunc[i][j],
				deepCopy, "%v")); err != nil {
				t.Fatal(err)
			}

			// 1. Download dependencies.
			if !rcDownloadMod(t, dir) {
				t.Skip("Skip TestReplaceConv due to network issues.")
			}
			opt := []string{"0", "1"}
			for k := range opt {
				// 2. Run.
				stdout, stderr, err := execCmd(dir, "", testenv.GoToolPath(t), "test", "-a", "-c", "-gcflags",
					"all=-beast=conv="+opt[k]+" -m", "-opt=beast", "-o", "replaceconv.test")
				if err != nil {
					t.Fatal(err)
				}

				if rcWrapperFunc[i] != "" && opt[k] == "1" {
					stdout, _, err = execCmd(dir, "", testenv.GoToolPath(t), "tool", "objdump", "-s", rcWrapperFunc[i], "replaceconv.test")
					if err != nil {
						t.Fatal(err)
					}
					checkWrapperPosition(t, stdout)
				}

				stdout, _, err = execCmd(dir, "", "./replaceconv.test", "-test.bench=ReplaceConv", "-test.benchmem", "-test.benchtime=1000x")
				if err != nil {
					t.Fatal(err)
				}

				// 3. Verification.
				optLog := fmt.Sprintf("replace_conv_test.go%s call to %s is replaced with tango/conv at compile time",
					rcLocations[i][j], rcImportMods[i]+"."+rcFuncs[i])
				switch opt[k] {
				case "1":
					if !strings.Contains(stderr, optLog) {
						t.Fatal("TestReplaceConv failed.")
					}
					if !strings.Contains(stdout, rcAllocations[i][j]+" allocs/op") {
						t.Fatal("TestReplaceConv failed.")
					}
					if !strings.Contains(stdout, "res is "+rcResults[i][j]) {
						fmt.Println(string(stdout))
						t.Fatal("TestReplaceConv failed.")
					}
				case "0":
					if strings.Contains(stderr, optLog) {
						t.Fatal("TestReplaceConv failed.")
					}
				}
			}
		}
	}
}

func checkWrapperPosition(t *testing.T, asm string) {
	// Check that the wrapper position is correct.
	for _, line := range strings.Split(asm, "\n") {
		if strings.Contains(line, "CALL runtime.morestack_noctxt.abi0(SB)") {
			pos := strings.Fields(line)[0]
			if pos != "<autogenerated>:1" {
				t.Errorf("wrapper position is incorrect: %s", pos)
			}
		}
	}
}

// Using the vendor model to use each library,
// the optimization should take effect.
func TestVendorMode(t *testing.T) {
	vendorDir := "conv_test"
	dir := t.TempDir()
	t.Logf("Working directory: %s\n", dir)
	goMod := path.Join(dir, "go.mod")
	p := path.Join(dir, "replace_conv_test.go")

	for i := range rcImportMods[:2] {
		// 1. Gen go.mod
		err := writeFile(goMod, fmt.Sprintf(rcGoModFile, rcImportMods[i], rcVersions[i]))
		if err != nil {
			t.Fatal(err)
		}

		// 2. Gen replace_conv_test.go
		deepCopy := getDeepCopyCode(i)
		if err := writeFile(p, fmt.Sprintf(rcSource, rcImports[i], "", deepCopy, "%v")); err != nil {
			t.Fatal(err)
		}

		// 3. Get dependencies
		if !rcDownloadMod(t, dir) {
			t.Skip("Skip TestVendorMode due to network issues.")
		}

		// 3. Make the vendor folder
		_, stderr, err := execCmd(dir, "", testenv.GoToolPath(t), "mod", "vendor", "-e", "-o", vendorDir)
		if err != nil {
			t.Fatal(err)
		}

		// 4. Replace original GitHub dependency with the vendor dependency.
		if err := writeFile(p, fmt.Sprintf(rcSource, "main/conv_test/"+rcImports[i], "", deepCopy, "%v")); err != nil {
			t.Fatal(err)
		}

		// 5. go mod tidy to remove unused modules
		if !rcDownloadMod(t, dir) {
			t.Skip("Skip TestVendorMode due to network issues.")
		}

		// 6. Build
		_, stderr, err = execCmd(dir, "", testenv.GoToolPath(t), "test", "-bench=ReplaceConv", "-test.benchmem",
			"-test.benchtime=1000x", "-gcflags", "all=-beast=conv -m")
		if err != nil {
			t.Fatal(stderr)
		}

		// 7. Verification
		optLog := fmt.Sprintf("replace_conv_test.go%s call to %s is replaced with tango/conv at compile time",
			rcLocations[i][0], rcImportMods[i]+"."+rcFuncs[i])
		if !strings.Contains(stderr, optLog) {
			t.Fatal("TestReplaceConv failed.")
		}
	}
}

func TestDeepCopyTime(t *testing.T) {
	dir := t.TempDir()
	t.Logf("Working directory: %s\n", dir)
	goMod := path.Join(dir, "go.mod")
	p := path.Join(dir, "replace_conv_test.go")

	source := `package deepcopy_test

import (
	"reflect"
    "runtime"
	"testing"
	"time"

	"github.com/mohae/deepcopy"
)

%s
`
	testcases := []string{
		`type A2 struct {
	*time.Time
	F1  int "json:\"-\""
	f2  int
}

func BenchmarkDeepCopyTimePointer(b *testing.B) {
    runtime.MemProfileRate = 1
	now := time.Now()
	a1 := A2{
		Time: &now,
		F1: 2,
		f2: 3,
	}
	var a2 interface{}
	for i := 0; i < b.N; i++ {
		a2 = deepcopy.Copy(&a1)
	}
    a3 := a2.(*A2)
	if !reflect.DeepEqual(a1.Time, a3.Time) || a1.F1 != a3.F1 || a3.f2 != 0 {
		b.Fatal("deepcopy failed.")
	}
}`, `type A1 struct {
	time.Time
	T time.Time
}

func BenchmarkDeepCopyTime(b *testing.B) {
    runtime.MemProfileRate = 1
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		b.Fatal(loc)
	}
	a1 := A1{
		Time: time.Now(),
		T:    time.Date(2024, 8, 1, 20, 28, 30, 20, loc),
	}
	var a2 interface{}
	for i := 0; i < b.N; i++ {
		a2 = deepcopy.Copy(a1)
	}

	if !reflect.DeepEqual(a1, a2) {
		b.Fatal("deepcopy failed.")
	}
}`,
	}
	loc := []string{":28:21:", ":29:21:"}
	alloc := []string{"3", "3"}
	for i := range testcases {
		err := writeFile(goMod, fmt.Sprintf(rcGoModFile, rcImportMods[0], rcVersions[0]))
		if err != nil {
			t.Fatal(err)
		}

		if err := writeFile(p, fmt.Sprintf(source, testcases[i])); err != nil {
			t.Fatal(err)
		}

		// 1. Download dependencies.
		if !rcDownloadMod(t, dir) {
			t.Skip("Skip TestDeepCopyTime due to network issues.")
		}

		// 2. Run.
		stdout, stderr, err := execCmd(dir, "", testenv.GoToolPath(t), "test", "-bench=.", "-test.benchmem",
			"-test.benchtime=1000x", "-gcflags", "all=-beast=conv -m")
		if err != nil {
			t.Fatal(stderr, stdout)
		}

		// 3. Verification.
		optLog := fmt.Sprintf("replace_conv_test.go%s call to %s is replaced with tango/conv at compile time",
			loc[i], rcImportMods[0]+"."+rcFuncs[0])
		if !strings.Contains(stderr, optLog) {
			t.Fatal("TestReplaceConv failed.")
		}
		if !strings.Contains(stdout, alloc[i]+" allocs/op") {
			t.Fatal("TestReplaceConv failed.")
		}
	}
}

func TestCopier(t *testing.T) {
	dir := t.TempDir()
	t.Logf("Working directory: %s\n", dir)
	goMod := path.Join(dir, "go.mod")
	p := path.Join(dir, "replace_conv_test.go")

	source := `package deepcopy_test

import (
	"reflect"
    "runtime"
	"testing"
	"time"
    "unsafe"

	"github.com/jinzhu/copier"
)

%s
`
	testcases := []string{
		`type A1 struct {
	t *time.Time
	time.Time
	F1 time.Time
	F2 unsafe.Pointer
	F3 complex64
	F4 complex128
	F5 chan int
	F6  int "json:\"-\""
	f7  int "json:\"-\""
	F8  int "copier:\"-\""
	f9  int "copier:\"-\""
}

func BenchmarkCopierOtherType(b *testing.B) {
	runtime.MemProfileRate = 1
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		b.Fatal(loc)
	}
	now := time.Now()
	a1 := A1{
		t:    &now,
		Time: time.Now(),
		F1:   time.Date(2024, 8, 1, 20, 28, 30, 20, loc),
		F2:   unsafe.Pointer(&now),
		F3:   complex(1, 2),
		F4:   complex128(3),
		F5:   make(chan int, 1),
		F6:   2,
		f7:   3,
		F8:   4,
		f9:   5,
	}
	var a2 interface{}
	for i := 0; i < b.N; i++ {
		copier.CopyWithOption(&a2, a1, copier.Option{DeepCopy: true})
	}
	a3 := a2.(A1)
	if a3.F8 != 0 {
		b.Fatal("deepcopy failed.")
	}
	a3.F8 = 4
	if !reflect.DeepEqual(a1, a3) {
		b.Fatal("deepcopy failed.")
	}
	var a4 *A1
	copier.CopyWithOption(&a4, &a1, copier.Option{DeepCopy: true})
	a4.F8 = 4
	if !reflect.DeepEqual(*a4, a1) {
		b.Fatal("deepcopy failed.")
	}
	now = time.Now()
	if a3.t != a1.t {
		b.Fatal("deepcopy failed.")
	}
}`, `type A1 struct {
	m *time.Time
	time.Time
	T  time.Time
	F2 unsafe.Pointer
}

type A2 struct {
	m *time.Time
	time.Time
	T  time.Time
	F2 unsafe.Pointer
}

func BenchmarkCopierDiffType(b *testing.B) {
	runtime.MemProfileRate = 1
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		b.Fatal(loc)
	}
	now := time.Now()
	a1 := A1{
		m:    &now,
		Time: time.Now(),
		T:    time.Date(2024, 8, 1, 20, 28, 30, 20, loc),
		F2:   unsafe.Pointer(&now),
	}
	var a2 A2
	for i := 0; i < b.N; i++ {
		copier.CopyWithOption(&a2, a1, copier.Option{DeepCopy: true})
	}
	reflect.TypeOf(a1)
	if a2.m != nil || a1.Time != a2.Time ||
		a1.T != a2.T || a1.F2 != a2.F2 {
		b.Fatal("deepcopy failed.")
	}
}`, `type A1 struct {
	F1 func() int
	F2 unsafe.Pointer
}

func BenchmarkCopierFuncType(b *testing.B) {
	runtime.MemProfileRate = 1
	now := time.Now()
	a1 := A1{
		F1: func() int {
			return 5
		},
		F2: unsafe.Pointer(&now),
	}
	var a2 interface{}
	for i := 0; i < b.N; i++ {
		copier.CopyWithOption(&a2, a1, copier.Option{DeepCopy: true})
	}
	reflect.TypeOf(a1)
	a3 := a2.(A1)
	if a1.F1() != a3.F1() || a1.F2 != a3.F2 {
		b.Fatal("deepcopy failed.")
	}
}`, `type A1 struct {
	F1 []string
	F2 map[string]interface{}
	f3 []string
	f4 map[string]string
}

var f = copier.CopyWithOption

func BenchmarkCopierNil(b *testing.B) {
	runtime.MemProfileRate = 1
	time.Now()
	a1 := A1{}
	var a2 A1
	var a3 A1
	for i := 0; i < b.N; i++ {
		f(&a2, a1, copier.Option{DeepCopy: true}) // not optimized.
	}
	reflect.TypeOf(unsafe.Pointer(&a1))
	copier.CopyWithOption(&a3, a1, copier.Option{DeepCopy: true}) // optimized.
	if a2.F1 == nil || a3.F1 == nil || a2.F2 == nil ||
		a3.F2 == nil {
		b.Fatal("deepcopy failed.")
	}
	if a2.f3 != nil || a3.f3 != nil || a2.f4 != nil ||
		a3.f4 != nil {
		b.Fatal("deepcopy failed.")
	}
	a3.F2["aa"] = "aa"
}`,
		`type A1 *A1

func BenchmarkCopierMultiLevelPtrType(b *testing.B) {
	runtime.MemProfileRate = 1
	a1 := new(A1)
	var a2 A1
	if err := copier.CopyWithOption(&a2, &a1, copier.Option{DeepCopy: true}); err == nil {
		b.Fatal("deepcopy failed.")
	}
	for i := 0; i < b.N; i++ {
		copier.CopyWithOption(&a2, &a1, copier.Option{DeepCopy: true})
	}
	time.Now()
	reflect.TypeOf(unsafe.Pointer(a1))
	if a2 != nil {
		b.Fatal("deepcopy failed.")
	}
}
`, `type A1 struct {
	F1 []string
	F2 map[string]interface{}
	f3 []string
	f4 map[string]string
}

var f = copier.CopyWithOption

func BenchmarkCopierNil(b *testing.B) {
	runtime.MemProfileRate = 1
	time.Now()
	a1 := A1{}
	var a2 A1
	for i := 0; i < b.N; i++ {
		copier.CopyWithOption(&a2, a1, copier.Option{DeepCopy: true, IgnoreEmpty: true})
	}
	reflect.TypeOf(unsafe.Pointer(&a1))
}`,
	}
	loc := []string{":49:24:", "", ":29:24:", ":32:23:", "", ":28:24:"}
	alloc := []string{"1", "33", "1", "15", "2", "9"}
	for i := range testcases {
		err := writeFile(goMod, fmt.Sprintf(rcGoModFile, rcImportMods[0], rcVersions[0]))
		if err != nil {
			t.Fatal(err)
		}

		if err := writeFile(p, fmt.Sprintf(source, testcases[i])); err != nil {
			t.Fatal(err)
		}

		// 1. Download dependencies.
		if !rcDownloadMod(t, dir) {
			t.Skip("Skip TestCopier due to network issues.")
		}

		// 2. Run.
		stdout, stderr, err := execCmd(dir, "", testenv.GoToolPath(t), "test", "-bench=.", "-test.benchmem",
			"-test.benchtime=1000x", "-gcflags", "all=-beast=conv -m")
		if err != nil {
			t.Fatal(stderr)
		}

		// 3. Verification.
		if loc[i] != "" {
			optLog := fmt.Sprintf("replace_conv_test.go%s call to %s is replaced with tango/conv at compile time",
				loc[i], rcImportMods[1]+"."+rcFuncs[1])
			if !strings.Contains(stderr, optLog) {
				t.Fatal("TestReplaceConv failed.")
			}
		} else {
			if strings.Contains(stderr, "is replaced with tango/conv at compile time") {
				t.Fatal("TestReplaceConv failed.")
			}
		}
		if !strings.Contains(stdout, alloc[i]+" allocs/op") {
			t.Fatal("TestReplaceConv failed.")
		}
	}
}
