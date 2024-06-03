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
		experimentPattern = `\w+(?:,\w+)*`
	)
	for _, separator := range []string{" X:", "-X:"} {
		if prefix, experiments, found := strings.Cut(bver, separator); found {
			bver = prefix
			if matched, err := regexp.MatchString(experimentPattern, experiments); err != nil {
				t.Fatalf("matching experiment features: %v", err)
			} else if !matched {
				t.Fatalf("experiment features %q do not match %q", experiments, experimentPattern)
			}
			break
		}
	}

	const versionPattern = `^go[123]\.\d+(?:rc\d+)?(?:\.\d{1,2})*\.bm\.\d+\.\d+\.(?:devel(?: .+)?|\d+(?:_preview)?)$`
	if matched, err := regexp.MatchString(versionPattern, bver); err != nil {
		t.Fatalf("matching BVersion: %v", err)
	} else if !matched {
		t.Fatalf("BVersion %q does not match %q", bver, versionPattern)
	}
}
