// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tango

import (
	"internal/testenv"
	"os/exec"
	"strings"
	"testing"
)

func TestGOTOOLCHAINEnv(t *testing.T) {
	testenv.MustHaveGoBuild(t)
	// test default value.
	expect := "local"
	cmd := exec.Command(testenv.GoToolPath(t), "env", "GOTOOLCHAIN")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(string(out))
	}
	outputStr := strings.TrimSpace(string(out))
	if outputStr != expect {
		t.Errorf("%s: got %q; want %q", "GOTOOLCHAIN", outputStr, expect)
	}

	// test with user’s environment.
	expect = "auto"
	cmd2 := exec.Command(testenv.GoToolPath(t), "env", "GOTOOLCHAIN")
	cmd2.Env = append(cmd2.Env, "GOTOOLCHAIN="+expect)
	out, err = cmd2.Output()
	if err != nil {
		t.Fatal(string(out))
	}
	outputStr = strings.TrimSpace(string(out))
	if outputStr != expect {
		t.Errorf("%s: got %q; want %q", "GOTOOLCHAIN", outputStr, expect)
	}
}
