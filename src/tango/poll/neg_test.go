// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux || !amd64
// +build !linux !amd64

package poll

import (
	"internal/testenv"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNegative(t *testing.T) {
	testenv.MustHaveGoRun(t)
	cmd := exec.Command(testenv.GoToolPath(t), "build", filepath.Join("testdata", "link.go"),
		filepath.Join("testdata", "smoking.go"))
	out, err := cmd.CombinedOutput()

	if err == nil || !strings.Contains(string(out), "build constraints exclude all") {
		t.Errorf("Negative testing failed, error: %v, out: %s", err, string(out))
	}
}
