// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"errors"
	"fmt"
	"internal/testenv"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"tango/runtimex"
	"testing"
)

type GoidPkg struct {
	pkg     string
	version string
}

// Public pid repos covered by the intrinsic.
var goidPkgs = []GoidPkg{
	{"github.com/choleraehyq/pid", "v0.0.12"},
}

// Two ways to turn on beast intrinsics
var beastOpt = []string{
	"-opt=beast",
	"-gcflags=all='-beast=intrinsic'",
}

const (
	goMod = "go.mod"
	main  = "main.go"
	bin   = "main"
)

func genGoMod(dir, pkg, version string) {
	path := path.Join(dir, goMod)
	source := `module main

go 1.16

require %s %s

`
	writeFile(path, []byte(fmt.Sprintf(source, pkg, version)))
}

func genGetGoIdMain(dir, pkg string) {
	path := path.Join(dir, main)
	source := `package main

import (
	p "%s"
	"runtime"
	"sync"
	"time"
)

const length = 1024

var gids [length]int64
var pids [length]int
var slows [length]int64

func main() {
	var wg sync.WaitGroup
	for i := 0; i < length; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			gid := p.Get()
			pid := p.GetPid()
			var buf [64]byte
			slow := p.ExtractGID(buf[:runtime.Stack(buf[:], false)])
			if gid != slow {
				panic("Incorrect gid")
			}
			gids[i] = gid
			pids[i] = pid
			slows[i] = slow
			time.Sleep(100 * time.Millisecond)
		}()
	}
	wg.Wait()
}
`
	writeFile(path, []byte(fmt.Sprintf(source, pkg)))
}

func writeFile(path string, content []byte) error {
	return ioutil.WriteFile(path, content, 0755)
}

var getGId = ".+CALL\\s+runtime\\.getGId.+"
var getPId = ".+CALL\\s+runtime\\.getPId.+"

// Find following instructions
// CALL runtime.getGId
// CALL runtime.getPId
func find(lines []string, regExp *regexp.Regexp) bool {
	for _, line := range lines {
		if regExp.MatchString(line) {
			return true
		}
	}
	return false
}

func verify(out string) {
	lines := strings.Split(out, "\n")
	// CALL runtime.getGId
	var ggoidRegExp = regexp.MustCompile(getGId)
	if !find(lines, ggoidRegExp) {
		log.Fatalf("%s", ggoidRegExp)
	}
	// CALL runtime.getPId
	var gmRegExp = regexp.MustCompile(getPId)
	if !find(lines, gmRegExp) {
		log.Fatalf("Fail to find loading g_m instruction")
	}
}

// Temporarily unset GOENV so the test can use the configured module proxy.
func processOSEnv() []string {
	osEnvs := os.Environ()
	clean := make([]string, 0, cap(osEnvs))
	for _, env := range osEnvs {
		if env == "GOENV=off" {
			continue
		}
		clean = append(clean, env)
	}
	return clean
}

// Some test cases rely on pid library and download it to the GOPATH.
// all.bash specify a non-existent GOPATH so that downloading fails.
// Generate a path in the temp directory and remove it once finished.
func genEnv(dir string) (env []string) {
	modDir := path.Join(dir, "gopath")
	cacheDir := path.Join(modDir, "cache")

	env = append(env, processOSEnv()...)
	env = append(env, "GOPATH="+modDir)
	env = append(env, "GOCACHE="+cacheDir)
	env = append(env, "GOMODCACHE="+cacheDir)
	return
}

func TestGetGoId(t *testing.T) {
	if *flagQuick {
		t.Skip("-quick")
	}
	for _, p := range goidPkgs {
		dir := t.TempDir()

		t.Logf("Working directory: %s\n", dir)
		genGoMod(dir, p.pkg, p.version)
		genGetGoIdMain(dir, p.pkg)
		env := genEnv(dir)

		// 1. Download dependencies
		cmd := exec.Command(testenv.GoToolPath(t), "mod", "download", p.pkg)
		cmd.Env = env
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Skip("TestGetGoId() is skipped due to the network issue.")
		}
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			return os.Chmod(path, 0777)
		})

		for _, opt := range beastOpt {
			// 2. Build
			cmd = exec.Command(testenv.GoToolPath(t), "build", opt, "-o", bin)
			t.Logf("  Build command %s", cmd.String())
			cmd.Env = env
			cmd.Dir = dir
			out, err = cmd.CombinedOutput()
			if err != nil {
				log.Fatal(string(out))
			}
			if _, err := os.Stat(path.Join(dir, bin)); errors.Is(err, os.ErrNotExist) {
				log.Fatalf("go build failed")
			}

			// 3. Run
			cmd = exec.Command(testenv.GoToolPath(t), "run", opt, main)
			t.Logf("  Execution command %s", cmd.String())
			cmd.Env = env
			cmd.Dir = dir
			out, err = cmd.CombinedOutput()
			if err != nil {
				log.Fatal(string(out))
			}

			// 4. objdump and verification
			cmd = exec.Command(testenv.GoToolPath(t), "tool", "objdump", bin)
			cmd.Env = env
			cmd.Dir = dir
			out, err = cmd.CombinedOutput()
			if err != nil {
				log.Fatal("objdump failed.")
			}
			verify(string(out))
		}
	}
}

// Obtain the pid in an infinite loop.
// First, the program will not crash, making sure that obtaining the pid is not preempted.
// Second, the program terminates iff when the main goroutine terminates, making sure that
// the infinite loop can be preempted, i.e., only instructions that obtain the pid are marked
// as unsafe to be preempted rather than the whole function.
func genGoIdAsyncPreemptionMain(dir, pkg string) {
	path := path.Join(dir, main)
	source := `package main

import (
	pid "%s"
)

const length = 1024
const Gs = 5 * 1000

var pids [length]int

func main() {
	for i := 0; i < Gs; i++ {
		i := i
		go func() {
			for {
				pids[i%%length] = pid.GetPid()
			}
		}()
	}
}
`
	writeFile(path, []byte(fmt.Sprintf(source, pkg)))
}

func TestGoIdAsyncPreemption(t *testing.T) {
	if *flagQuick {
		t.Skip("-quick")
	}
	dir := t.TempDir()

	t.Logf("Working directory: %s\n", dir)
	p := goidPkgs[0]
	genGoMod(dir, p.pkg, p.version)
	genGoIdAsyncPreemptionMain(dir, p.pkg)
	env := genEnv(dir)

	// 1. Download dependencies
	cmd := exec.Command(testenv.GoToolPath(t), "mod", "download", p.pkg)
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skip("TestGetGoId() is skipped due to the network issue.")
	}
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		return os.Chmod(path, 0777)
	})

	// 2. Run
	cmd = exec.Command(testenv.GoToolPath(t), "run", "-opt=beast", main)
	cmd.Env = env
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		log.Fatal(string(out))
	}
}

// We use the exact import path to match the pid calls.
// In this patch, we use vendor mode, i.e., cloning the pid
// lib to the vendor directory and import from that directory.
// Then the package name will not be "github.com/choleraehyq/pid",
// and this case are expected not to be handled.
func TestGoIdNameMatching(t *testing.T) {
	if *flagQuick {
		t.Skip("-quick")
	}
	vendorDir := "pid_test"

	dir := t.TempDir()
	env := genEnv(dir)
	t.Logf("Working directory: %s\n", dir)

	// 1. Gen go.mod
	gomodPath := path.Join(dir, goMod)
	source := `module main
go 1.18
require github.com/choleraehyq/pid v0.0.18
`
	writeFile(gomodPath, []byte(source))

	// 2. Gen main.go
	mainPath := path.Join(dir, main)
	source = `
package main

import (
	pid "github.com/choleraehyq/pid"
	"runtime"
)

func main() {
	println(pid.GetPid())
	println(pid.Get())
	var buf [64]byte
	println(pid.ExtractGID(buf[:runtime.Stack(buf[:], false)]))
}
`
	writeFile(mainPath, []byte(source))

	// 3. Get dependencies
	cmd := exec.Command(testenv.GoToolPath(t), "get")
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skip("TestGetGoId() is skipped due to the network issue.")
	}

	// 3. Make the vendor folder
	cmd = exec.Command(testenv.GoToolPath(t), "mod", "vendor", "-e", "-o", vendorDir)
	cmd.Env = env
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatal(string(out))
	}

	// 4. Replace original GitHub dependency with the vendor dependency.
	newSource := strings.Replace(source,
		"pid \"github.com/choleraehyq/pid\"",
		"pid \"main/pid_test/github.com/choleraehyq/pid\"", 1)
	writeFile(mainPath, []byte(newSource))

	// 5. go mod tidy to remove unused modules
	cmd = exec.Command(testenv.GoToolPath(t), "mod", "tidy")
	cmd.Env = env
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatal(string(out))
	}

	// 6. Build
	for _, opt := range beastOpt {
		cmd = exec.Command(testenv.GoToolPath(t), "build", opt, "-o", bin)
		t.Logf("  Build command %s", cmd.String())
		cmd.Env = env
		cmd.Dir = dir
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Log(string(out))
			continue
		}
		if _, err := os.Stat(path.Join(dir, bin)); errors.Is(err, os.ErrNotExist) {
			t.Log("go build failed")
			continue
		}

		// 7. objdump and verification
		cmd = exec.Command(testenv.GoToolPath(t), "tool", "objdump", bin)
		cmd.Env = env
		cmd.Dir = dir
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatal("objdump failed.")
		}

		// pid calls are not matched, so replacement cannot happen.
		verifyNotContain := func(out string) {
			lines := strings.Split(out, "\n")
			// CALL runtime.getGId
			var getGid = regexp.MustCompile(getGId)
			if find(lines, getGid) {
				t.Fatalf("%s is found.", getGId)
			}
			// CALL runtime.getPId
			var getPid = regexp.MustCompile(getPId)
			if find(lines, getPid) {
				t.Fatalf("%s is found.", getPid)
			}
		}
		verifyNotContain(string(out))
	}

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		return os.Chmod(path, 0777)
	})
}

// Test the validity of tango/goid.Get()
// Compare the results of tango/goid.Get() against runtime.GetGId().
func TestGoIdAPI(t *testing.T) {
	const length = 1000

	var api [length]int64
	var internal [length]int64
	var wg sync.WaitGroup

	for i := 0; i < length; i++ {
		i := i
		wg.Add(1)
		go func() {
			api[i] = runtimex.GetGId()
			internal[i] = runtime.GetGIdInternal()
			wg.Done()
		}()
	}

	wg.Wait()

	for i := 0; i < length; i++ {
		if api[i] != internal[i] {
			t.Fatalf("gid obtained by two ways are not the same %d, %d", api[i], internal[i])
		}
	}
}
