// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"fmt"
	"internal/testenv"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

// create temporary work dir and create a source file with given content
func prepareSource(t *testing.T, filename string, content []byte) (workdir string, err error) {
	workdir = t.TempDir()
	if e := os.MkdirAll(workdir, 0755); e != nil {
		return "", e
	}

	fullPath := fmt.Sprintf("%s/%s", workdir, filename)
	err = os.WriteFile(fullPath, content, 0644)
	return workdir, err
}

func TestBeast(t *testing.T) {
	testenv.MustHaveGoRun(t)

	source := `package main

//go:noinline
func foo2(a int) int {
	return a + 1

}

// AST has 26nodes(except callnode), and call cost is 57 by default.
// so the total inline cost of func bigcost() is 83
func bigcost(b, c int) int {
	a := b + c

	a = a + c
	a = b + c
	a = a + c
	return foo2(a)
}

func main() {
	bigcost(10, 10)
}
`
	filename := "beast.go"
	workdir, err := prepareSource(t, filename, []byte(source))
	if workdir != "" {
		defer os.RemoveAll(workdir)
	}
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created workdir: %s\n", workdir)

	beastArgs := append([]string{"build", "-opt=beast", "-x", "-gcflags=all=-m -m"}, filename)
	nobeastArgs := append([]string{"build", "-x", "-gcflags=all=-m -m"}, filename)

	haveInlined := regexp.MustCompile(`: inlining call to bigcost`)
	stacksize := regexp.MustCompile(`runtime/stacksizebeast.go ([^ ]*)`)

	for i := 0; i < 2; i++ {
		// run go build -gcflags=all=-m -m beast.go.
		args := beastArgs
		if i == 1 {
			args = nobeastArgs
		}
		cmd := testenv.CleanCmdEnv(exec.Command(testenv.GoToolPath(t), args...))
		cmd.Dir = workdir
		cmd.Env = append(os.Environ(), "GOCACHE="+workdir)

		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw
		cmdErr := make(chan error, 1)
		go func() {
			cmdErr <- cmd.Run()
			pw.Close()
		}()
		scanner := bufio.NewScanner(pr)

		inlined := false
		tags := false

		for scanner.Scan() {
			line := scanner.Text()
			if m := haveInlined.FindStringSubmatch(line); m != nil {
				inlined = true
				continue
			}

			if n := stacksize.FindStringSubmatch(line); n != nil {
				tags = true
				continue
			}
		}

		if err := <-cmdErr; err != nil {
			t.Fatal(err)
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if !inlined {
				t.Errorf("beast not work for inlining")
			}
			if !tags {
				t.Errorf("beast not work for tags")
			}
		} else {
			if inlined {
				t.Errorf("default mode did the inilining")
			}
		}
		pr.Close()
	}

}

//go:noinline
func foo2(a int) int {
	return a + 1
}

// cost is around 83
func bigcost(b, c int) int {
	a := b + c

	a = a + c
	a = b + c
	a = a + c
	return foo2(a)
}

func BenchmarkBeastInline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		num1 := 1
		num2 := 1
		bigcost(num1, num2)
	}
}

func TestBeastStackSizeValue(t *testing.T) {
	testenv.MustHaveGoRun(t)

	const source = `
package main

import (
        "fmt"
        "sync"
        "unsafe"
)

var startAddr uintptr
var endAddr uintptr

func main() {
        var wg sync.WaitGroup
        wg.Add(1)
        go foo(&wg)

        wg.Wait()

        if endAddr > startAddr || startAddr-endAddr > 2048  {
                fmt.Println("stackcopy on this goroutine")
		panic("stackcopy happens")
        } else {
		fmt.Println("nostackcopy on this goroutine")
        }
}

//go:noinline
func foo(wg *sync.WaitGroup) {
	a := 10
	startAddr = uintptr(unsafe.Pointer(&a))
	x := 0
	// the total framesize of recursive() is 48
	// if stacksize is 2048, on go1.16.4 linux, stackgurd0
	// is stack.lo + 928 on linux. so 25*48 has reach
	// stackguard0, and stackcopy is needed
	n := 25
	recursive(&x, n)
	wg.Done()
}

//go:noinline
func recursive(p *int, n int) {
	if n == 0 {
		b := 10
		if uintptr(unsafe.Pointer(&b)) > endAddr {
			endAddr = uintptr(unsafe.Pointer(&b))
		}
		*p = n + 1
		return
	}
	*p = n + 1
	x := 0
	recursive(&x, n-1)
}
`
	fname := "beastStacksize.go"
	workdir, err := prepareSource(t, fname, []byte(source))
	if workdir != "" {
		defer os.RemoveAll(workdir)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Created workdir: %s\n", workdir)

	// run go build -opt=beast beastStacksize.go -o beast
	cmd := exec.Command("go", "build", "-opt=beast", "-o", "beast", fname)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("beast mode compile failed with %s\n", out)
	}

	cmd2 := exec.Command("go", "build", "-o", "nobeast", fname)
	cmd2.Dir = workdir
	out2, err2 := cmd2.CombinedOutput()
	if err2 != nil {
		log.Fatalf("no beast mode compile failed with %s\n", out2)
	}

	beastcmd := exec.Command("./beast")
	beastcmd.Dir = workdir
	beastout, beasterr := beastcmd.CombinedOutput()
	if beasterr != nil {
		log.Fatalf("beast mode execute failed with %s\n", beastout)
	}

	nobeastcmd := exec.Command("./nobeast")
	nobeastcmd.Dir = workdir
	_, nobeasterr := nobeastcmd.CombinedOutput()
	// nobeasterr should not be nil
	if nobeasterr == nil {
		log.Fatalf("nobeast mode should have panic but not\n")
	}
}

//go:noinline
func growStack(wg *sync.WaitGroup) {
	n := 25
	for i := 0; i < n; i++ {
		x := 0
		growStackIter(&x, n, wg)
		if x != n+1 {
			panic("stack is corrupted")
		}
	}
	wg.Done()
}

//go:noinline
func growStackIter(p *int, n int, wg *sync.WaitGroup) {
	if n == 0 {
		*p = n + 1
		return
	}
	*p = n + 1
	x := 0
	growStackIter(&x, n-1, wg)
	if x != n {
		panic("stack is corrupted")
	}
}

func BenchmarkBeastStacksize(b *testing.B) {
	var wg sync.WaitGroup

	wg.Add(b.N)
	for i := 0; i < b.N; i++ {
		go growStack(&wg)
	}
}

func BenchmarkBeastbce(b *testing.B) {
	for i := 0; i < b.N; i++ {
		runtime.GC()
	}
}

var refs []unsafe.Pointer

func BenchmarkStaticSmallGarbage(b *testing.B) {
	refs = make([]unsafe.Pointer, 7)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		refs[0] = unsafe.Pointer(&[1]byte{})
		refs[1] = unsafe.Pointer(&[99]byte{})
		refs[2] = unsafe.Pointer(&[8]int8{})
		refs[3] = unsafe.Pointer(&[10]uint{})
		refs[4] = unsafe.Pointer(&[5]uint16{})
		refs[5] = unsafe.Pointer(&[2]float32{})
		refs[6] = unsafe.Pointer(&[2]struct {
			a int
			c byte
			d bool
			e uint16
		}{})
	}
	b.StopTimer()
}

func TestGabInline(t *testing.T) {
	testenv.MustHaveGoRun(t)

	source := `package main
import (
	"unsafe"
	"fmt"
	"os"
)

var refs []unsafe.Pointer

func main() {
	refs = make([]unsafe.Pointer, 2)

	if os.Getenv("GODEBUG") != "gab=1" {
		panic("env doesn't have GODEBUG=gab=1")
	}

	// newobject should be gab-inlined.
	refs[0] = unsafe.Pointer(&(new([16]byte)[0]))
	refs[1] = unsafe.Pointer(&(new([64]byte)[0]))

	addr1 := uintptr(refs[0])
	addr2 := uintptr(refs[1])
	if addr1 > addr2 || addr2 - addr1 > 16 {
		panic(fmt.Sprintf("addr1 (0x%x) addr2 (0x%x) should be in same GAB\n", addr1, addr2))
	}
}
`
	filename := "gabinline.go"
	workdir, err := prepareSource(t, filename, []byte(source))
	if workdir != "" {
		defer os.RemoveAll(workdir)
	}
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created workdir: %s\n", workdir)

	cmd := exec.Command(testenv.GoToolPath(t), "run", "-opt=beast", filename)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "GODEBUG=gab=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("beast mode compile failed with %s\n", out)
	}
}

// TestGabInlineGabDisabled tests the cases where beast.gabInline=1 and gabEnabled=false.
func TestGabInlineGabDisabled(t *testing.T) {
	testenv.MustHaveGoRun(t)

	source := `package main
import (
	"unsafe"
	"fmt"
	"os"
)

var refs []unsafe.Pointer

func main() {
	refs = make([]unsafe.Pointer, 2)

	if os.Getenv("GODEBUG") != "gab=0" {
		panic("env doesn't have GODEBUG=gab=0")
	}

	refs[0] = unsafe.Pointer(&(new([16]byte)[0]))
	refs[1] = unsafe.Pointer(&(new([64]byte)[0]))

	addr1 := uintptr(refs[0])
	addr2 := uintptr(refs[1])
	if !(addr1 > addr2 || addr2 - addr1 > 16) {
		panic(fmt.Sprintf("addr1 (0x%x) addr2 (0x%x) should not be in same GAB\n", addr1, addr2))
	}
}
`
	filename := "gabInlineGabDisabled.go"
	workdir, err := prepareSource(t, filename, []byte(source))
	if workdir != "" {
		defer os.RemoveAll(workdir)
	}
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Created workdir: %s\n", workdir)

	cmd := exec.Command(testenv.GoToolPath(t), "run", "-opt=beast", filename)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "GODEBUG=gab=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("beast mode compile failed with %s\n", out)
	}
}
