// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"internal/testenv"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const (
	mainFileStr = "main.go"
	binStr      = "test"
)

func genTestCode(dir string) {
	genTestMain(dir)
}

func genTestMain(dir string) {
	path := path.Join(dir, mainFileStr)
	source := `package main

	func SliceClear(s []int) []int {
		for i := range s {
			s[i] = 0
		}
		return s
	}

	func SliceMakeCopyNoOptCap(s []int) []int {
		a := make([]int, len(s), 9)
		copy(a, s)
		return a
	}


	func main() {
		var s []int
		SliceClear(s)
		SliceMakeCopyNoOptCap(s)
	}
`
	outputFile(path, []byte(source))
}

func outputFile(path string, content []byte) error {
	return ioutil.WriteFile(path, content, 0755)
}

var memclrDisdinctInstr = ".+memclr_amd64\\.s.+MOVB runtime\\.memmoveBits.+"
var memmoveDisdictInstr = ".+memmove_amd64\\.s.+VMOVDQU X7, 0xe0.+"

// misleading instruction recognized by X86asm lib on VPXOR in runtime.memmove
var X86AsmMisleadingInstr1 = ".+memmove_amd64\\.s.+OUTL AX, DX.+"

// misleading instruction recognized by X86asm lib, which from next runtime
// function after runtime.memmove
var X86AsmMisleadingInstr2 = ".+memmove_amd64\\.s.+CALL.+"

func findAsmLine(lines []string, regExp *regexp.Regexp) bool {
	for _, line := range lines {
		if regExp.MatchString(line) {
			return true
		}
	}
	return false
}

func checkAssemblyExistency(out string, check string, t *testing.T, checkExistency bool) {
	lines := strings.Split(out, "\n")
	var regExp = regexp.MustCompile(check)
	if checkExistency {
		if !findAsmLine(lines, regExp) {
			t.Fatalf("%s", regExp)
		}
	} else {
		if findAsmLine(lines, regExp) {
			t.Fatalf("%s", regExp)
		}
	}
}

func genAndBuildSampleCode(t *testing.T, dir string) {
	genTestCode(dir)
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		return os.Chmod(path, 0777)
	})
	// 1. Build
	cmd := exec.Command(testenv.GoToolPath(t), "build", "-o", binStr, mainFileStr)
	t.Logf("  Build command %s", cmd.String())
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(string(out))
	}
	if _, err := os.Stat(path.Join(dir, binStr)); errors.Is(err, os.ErrNotExist) {
		t.Fatal("go build failed")
	}

	// 2. Build with -opt=beast
	cmd = exec.Command(testenv.GoToolPath(t), "build", "-opt=beast", "-o", binStr+"1", mainFileStr)
	t.Logf("  Execution command %s", cmd.String())
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatal(string(out))
	}
}

func TestMemmoveAndMemclrVersion(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("skipping on non-amd64 platforms")
	}
	dir := t.TempDir()
	t.Logf("Working directory: %s\n", dir)
	genAndBuildSampleCode(t, dir)
	// objdump and verification
	cmd := exec.Command(testenv.GoToolPath(t), "tool", "objdump", "-s", "runtime.memclrNoHeapPointers", binStr)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("objdump failed.")
	}
	checkAssemblyExistency(string(out), memclrDisdinctInstr, t, false)

	cmd = exec.Command(testenv.GoToolPath(t), "tool", "objdump", "-s", "runtime.memmove", binStr)
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatal("objdump failed.")
	}
	checkAssemblyExistency(string(out), memmoveDisdictInstr, t, false)

	cmd = exec.Command(testenv.GoToolPath(t), "tool", "objdump", "-s", "runtime.memclrNoHeapPointers", binStr+"1")
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatal("objdump failed.")
	}
	checkAssemblyExistency(string(out), memclrDisdinctInstr, t, true)

	cmd = exec.Command(testenv.GoToolPath(t), "tool", "objdump", "-s", "runtime.memmove", binStr+"1")
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatal("objdump failed.")
	}
	checkAssemblyExistency(string(out), memmoveDisdictInstr, t, true)
}

// This test is here to check whether VPXOR is used in memmove and been
// recognized by x86asm incorrectly.
// go tool objdump use x86asm module to disassemble in AMD64, who
// can't recognize VPXOR currently.
func TestX86asmFunctionalityOnMemmove(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("only run TestX86AsmFunctionaliyOnMemmove in amd64 architecture")
	}
	dir := t.TempDir()
	t.Logf("Working directory: %s\n", dir)
	genAndBuildSampleCode(t, dir)
	// objdump and verification
	cmd := exec.Command(testenv.GoToolPath(t), "tool", "objdump", "-s", "runtime.memmove", binStr+"1")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("objdump failed.")
	}
	checkAssemblyExistency(string(out), X86AsmMisleadingInstr1, t, false)
	checkAssemblyExistency(string(out), X86AsmMisleadingInstr2, t, false)
}
