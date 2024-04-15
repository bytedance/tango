// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pprof

import (
	"encoding/json"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	_ "unsafe" // for go:linkname
)

const (
	tangoInfoTag     = "TANGO_INFO_JSON" // begins with it
	tangoInfoVersion = "1.1"             // tango info version
)

// Save the result of runtime.GOROOT()
var tangoInfoGoRoot = runtime.GOROOT()

// Envs we'll check and dump.
var tangoInfoEnvs = []string{
	"GOGC",
	"GODEBUG",
	"MY_CPU_LIMIT",
	"MY_CPU_REQUEST",
}

//go:linkname stackMin runtime.getStackMin
func stackMin() int

// Encodes the comments for tangoInfo.
func (b *profileBuilder) pbTangoInfo() {
	// 1. Static information
	data := map[string]string{
		"TI_VERSION":   tangoInfoVersion,
		"BVERSION":     runtime.BVersion(),
		"GOROOT":       tangoInfoGoRoot,
		"GOMAXPROCS":   strconv.Itoa(runtime.GOMAXPROCS(-1)),
		"VCS_REVISION": getVCSRevision(),
		"STACK_MIN":    strconv.Itoa(stackMin()),
	}

	// 2. Envs once they exist.
	// Since some of these envs can change during runtime,
	// we collect it again.
	for _, envName := range tangoInfoEnvs {
		if envVal := os.Getenv(envName); envVal != "" {
			data[envName] = envVal
		}
	}

	cmt, err := genTangoInfoJson(&data)
	if err != nil {
		return
	}
	b.pb.int64(tagProfile_Comment, b.stringIndex(string(cmt)))
}

// Generate tangoInfo message in Json format.
// It's something like below:
//
// TANGO_INFO_JSON:{"TI_VERSION":"1.0", "KEY1":"VALUE1", "KEY2":"VALUE2"...}
//
// It stars with string: ^TANGO_INFO_JSON,
// indicating it's a special comment,
// then the actual data of key:value pairs
func genTangoInfoJson(data *map[string]string) (string, error) {
	// Transfer the data into jason format
	js, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	return tangoInfoTag + ":" + string(js), nil
}

// Saves the VCS Revision
var vcsRevision string
var vcsRevisionOnce sync.Once

// Fetchs the VCS Revision from ReadBuildInfo()
func getVCSRevision() string {
	vcsRevisionOnce.Do(func() {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					vcsRevision = strings.TrimSpace(setting.Value)
					return
				}
			}
		}
	})
	return vcsRevision
}
