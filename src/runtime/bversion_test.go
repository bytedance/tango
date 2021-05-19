// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestBVersion(t *testing.T) {
	bver := runtime.BVersion()
	if strings.HasPrefix(bver, "devel ") {
		t.Skip("development branch")
	}

	const (
		experimentSeparator = " X:"
		experimentPattern   = `\w+(?:,\w+)*`
	)
	if prefix, experiments, found := strings.Cut(bver, experimentSeparator); found {
		bver = prefix
		if matched, err := regexp.MatchString(experimentPattern, experiments); err != nil {
			t.Fatalf("matching experiment features: %v", err)
		} else if !matched {
			t.Fatalf("experiment features %q do not match %q", experiments, experimentPattern)
		}
	}

	const versionPattern = `^go[123]\.\d+(?:rc\d+)?(?:\.\d{1,2})*\.bm\.\d+\.\d+\.(?:devel(?: .+)?|\d+(?:_preview)?)$`
	if matched, err := regexp.MatchString(versionPattern, bver); err != nil {
		t.Fatalf("matching BVersion: %v", err)
	} else if !matched {
		t.Fatalf("BVersion %q does not match %q", bver, versionPattern)
	}
}
