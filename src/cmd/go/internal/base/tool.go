// Copyright 2017 The Go Authors. All rights reserved.
// Copyright (c) 2025 ByteDance Ltd. and/or its affiliates.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file has been modified by ByteDance Ltd. and/or its affiliates on 2025-07-30.

package base

import (
	"fmt"
	"go/build"
	"internal/buildcfg"
	"os"
	"path/filepath"

	"cmd/go/internal/cfg"
	"cmd/internal/par"
)

// select a suitable tool
func selectTool(toolName string) string {
	if buildcfg.Experiment.TangoLLVM &&
		os.Getenv("BUILDING_TANGOLLVM_TOOLS") != "1" &&
		buildcfg.GOOS == "linux" &&
		buildcfg.GOARCH == "amd64" &&
		(toolName == "compile" || toolName == "link") {
		return "llvm_" + toolName
	}
	return toolName
}

// Tool returns the path to the named builtin tool (for example, "vet").
// If the tool cannot be found, Tool exits the process.
func Tool(toolName string) string {
	toolName = selectTool(toolName)
	toolPath, err := ToolPath(toolName)
	if err != nil && len(cfg.BuildToolexec) == 0 {
		// Give a nice message if there is no tool with that name.
		fmt.Fprintf(os.Stderr, "go: no such tool %q\n", toolName)
		if toolName == "llvm_compile" || toolName == "llvm_link" {
			fmt.Fprintln(os.Stderr, "go: build a TangoLLVM toolchain as described in doc/tangollvm.md")
		}
		SetExitStatus(2)
		Exit()
	}
	return toolPath
}

// ToolPath returns the path at which we expect to find the named tool
// (for example, "vet"), and the error (if any) from statting that path.
func ToolPath(toolName string) (string, error) {
	if !ValidToolName(toolName) {
		return "", fmt.Errorf("bad tool name: %q", toolName)
	}
	toolPath := filepath.Join(build.ToolDir, toolName) + cfg.ToolExeSuffix()
	err := toolStatCache.Do(toolPath, func() error {
		_, err := os.Stat(toolPath)
		return err
	})
	return toolPath, err
}

func ValidToolName(toolName string) bool {
	for _, c := range toolName {
		switch {
		case 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '_':
		default:
			return false
		}
	}
	return true
}

var toolStatCache par.Cache[string, error]
