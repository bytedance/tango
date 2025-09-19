// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"text/template"
)

var (
	globalPluginFunc func()
	globalPluginType interface{}
	lastPlugin       *plugin.Plugin
	tmpDir           string
)

func Init(count int) (func(), error) {
	var err error
	tmpDir, err = os.MkdirTemp("", "plugins_test")
	if err != nil {
		return nil, err
	}
	clean := func() {
		os.RemoveAll(tmpDir)
	}
	fmt.Printf("create temp plugin dir: %s\n", tmpDir)

	for i := 1; i <= count; i++ {
		err := generatePlugin(tmpDir, i)
		if err != nil {
			return clean, err
		}
	}

	for i := 1; i <= count; i++ {
		err := compilePlugin(tmpDir, i)
		if err != nil {
			return clean, err
		}
	}
	return clean, nil
}

func loadAndRunPlugin(dir string, num int) error {
	pluginPath := filepath.Join(dir, fmt.Sprintf("plugin%d.so", num))
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return err
	}
	lastPlugin = p
	return nil
}

func checkLastPlugin() error {
	if lastPlugin == nil {
		return fmt.Errorf("last plugin not found")
	}
	sym, err := lastPlugin.Lookup("GetFunction")
	if err != nil {
		return err
	}
	sym, err = lastPlugin.Lookup("GetFunction")
	if err != nil {
		return fmt.Errorf("find GetFunction failed: %v", err)
	}

	getFunc, ok := sym.(func() interface{})
	if !ok {
		return fmt.Errorf("func assert failed")
	}
	funcInterface := getFunc()
	pluginFunc, ok := funcInterface.(func())
	if !ok {
		return fmt.Errorf("func convert failed")
	}
	globalPluginFunc = pluginFunc

	sym2, err := lastPlugin.Lookup("GetType")
	if err != nil {
		return fmt.Errorf("find GetType failed: %v", err)
	}

	getType, ok := sym2.(func() interface{})
	if !ok {
		return fmt.Errorf("func assert failed")
	}
	typ := getType()
	globalPluginType = typ
	return nil
}

const pluginTemplate = `package main

import "fmt"

type MyStruct struct {
	Name string
}

func {{.FuncName}}() {
    fmt.Println("{{.Message}}")
}

// export function
func GetFunction() interface{} {
    return {{.FuncName}}
}

func GetType() interface{} {
    return &MyStruct{}
}
`

type PluginData struct {
	FuncName string
	Message  string
}

func generatePlugin(dir string, num int) error {
	pluginFile := filepath.Join(dir, fmt.Sprintf("plugin%d.go", num))
	f, err := os.Create(pluginFile)
	if err != nil {
		return err
	}
	defer f.Close()

	data := PluginData{
		FuncName: fmt.Sprintf("Plugin%dFunc", num),
		Message:  fmt.Sprintf("Hello from Plugin %d!", num),
	}
	tpl := template.Must(template.New("plugin").Parse(pluginTemplate))
	err = tpl.Execute(f, data)
	if err != nil {
		return err
	}
	return nil
}

func compilePlugin(dir string, num int) error {
	pluginSource := filepath.Join(dir, fmt.Sprintf("plugin%d.go", num))
	pluginOutput := filepath.Join(dir, fmt.Sprintf("plugin%d.so", num))
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginOutput, pluginSource)
	cmd.Env = append(os.Environ(), "GO111MODULE=auto")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compile error: %s", string(output))
	}
	return nil
}
