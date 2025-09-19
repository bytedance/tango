// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestModuledataHitMiss(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("skipping test on %s", runtime.GOOS)
	}
	// src/runtime/crash_test.go:184 will add goroutineleakprofile.
	// Set default experiment to empty when it is removed.
	experiment := "goroutineleakprofile"
	if envExperiment := os.Getenv("GOEXPERIMENT"); envExperiment != "" {
		experiment += "," + envExperiment
	}
	envs := [][]string{
		// disable asyncpreempt due to traceback runtime.nanotime will trigger unexpected search miss.
		{
			"GODEBUG=moduledataopt=3,moduledatadebug=2,asyncpreemptoff=1",
			"MODULEDATACOUNT=1",
			"GOEXPERIMENT=" + experiment,
		},
	}
	if !testing.Short() {
		envs = append(envs, []string{
			"GODEBUG=moduledataopt=3,moduledatadebug=2,asyncpreemptoff=1",
			"MODULEDATACOUNT=100",
			"GOEXPERIMENT=" + experiment,
		})
	}
	var (
		stage     = 0
		lastHit   = 0
		lastMiss  = 0
		currHit   = 0
		currMiss  = 0
		checkHit  = 0
		checkMiss = 0
	)
	reset := func() {
		stage = 0
		lastHit = 0
		lastMiss = 0
		currHit = 0
		currMiss = 0
		checkHit = 0
		checkMiss = 0
	}
	checker := func(l string) error {
		currHit = 0
		currMiss = 0
		defer func() {
			if currHit != 0 {
				lastHit = currHit
			}
			if currMiss != 0 {
				lastMiss = currMiss
			}
		}()
		// check for moduledatadebug output.
		if strings.HasPrefix(l, "[ModuleDebug]") {
			if strings.Contains(l, "not sorted") {
				return fmt.Errorf("[ModuleDebug] not sorted %s", l)
			}
			fields := strings.Fields(l)
			if len(fields) > 2 {
				switch fields[1] {
				case "moduleDataSearchHit:":
					if val, err := strconv.Atoi(fields[2]); err != nil {
						return fmt.Errorf("invalid value for moduleDataSearchHit: %v", fields[2])
					} else {
						currHit = val
					}
				case "moduleDataSearchMiss:":
					if val, err := strconv.Atoi(fields[2]); err != nil {
						return fmt.Errorf("invalid value for moduleDataSearchMiss: %v", fields[2])
					} else {
						currMiss = val
					}
				default:
				}
			}
		}
		// check for main error.
		if strings.HasPrefix(l, "[Main] err:") {
			return fmt.Errorf("%s", l)
		}
		switch stage {
		case 0: // init
			if l == "[Main] update to hit" {
				stage = 1
				checkHit = lastHit
				checkMiss = lastMiss
				return nil
			}
		case 1: // search hit
			if currMiss != 0 && currMiss != checkMiss {
				return fmt.Errorf("unexpect miss moduledata search want: %d get %d", checkMiss, currMiss)
			}
			if currHit != 0 && currHit < checkHit {
				return fmt.Errorf("unexpect hit moduledata search want:>= %d get %d", checkHit, currHit)
			}
			if l == "[Main] update to miss" {
				stage = 2
				checkHit = lastHit
				checkMiss = lastMiss
				return nil
			}
		case 2: // search miss
			if currMiss != 0 && currMiss <= checkMiss {
				return fmt.Errorf("unexpect miss moduledata search want:> %d get %d", checkMiss, currMiss)
			}
		default:
			return nil
		}
		return nil
	}
	var err error
	for _, env := range envs {
		reset()
		output := runTestProg(t, "testmoduledata", "ModuleDataOptHitMiss", env...)
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			err = checker(line)
			if err != nil {
				t.Log("env:", env)
				t.Logf("output:%s", output)
				t.Errorf("checker got error: %s", err)
				break
			}
		}
	}
}

func TestModuledataQuery(t *testing.T) {
	if runtime.IsModuleDataOptEnabled() {
		t.Skipf("skipping test due to it will reset ModuleDataOpt data")
	}
	defer runtime.MockResetModuleDataOpt()
	mds := make([]*runtime.MockModuleData, 0, 10)
	mds = append(mds, &runtime.MockModuleData{
		Minpc:      923765312, // avoid duplication with firstmoduledata
		Maxpc:      1318361269,
		Types:      1334680480,
		Etypes:     1511626132,
		Gofunc:     1442516824,
		End:        2106330320,
		Pluginpath: "",
		Modulename: "",
		Hasmain:    1,
		Bad:        false,
	})
	mds = append(mds, &runtime.MockModuleData{
		Minpc:      139825684190240,
		Maxpc:      139825685082093,
		Types:      139825685286720,
		Etypes:     139825685478360,
		Gofunc:     139825685125296,
		End:        139825688578160,
		Pluginpath: "plugin/unnamed-65940cbaacda62e30b77197aec39523997bf45c5",
		Modulename: "",
		Hasmain:    0,
		Bad:        false,
	})
	mds = append(mds, &runtime.MockModuleData{
		Minpc:      139825206068256,
		Maxpc:      139825206960109,
		Types:      139825207164736,
		Etypes:     139825207356376,
		Gofunc:     139825207003312,
		End:        139825210456176,
		Pluginpath: "plugin/unnamed-43774843a65f3c64c3ffb2df6f3eab8c028ff40f",
		Modulename: "",
		Hasmain:    0,
		Bad:        true,
	})
	mds = append(mds, &runtime.MockModuleData{
		Minpc:      139825201083424,
		Maxpc:      139825201975277,
		Types:      139825202179904,
		Etypes:     139825202371544,
		Gofunc:     139825202018480,
		End:        139825205471344,
		Pluginpath: "plugin/unnamed-c3118498da6800a47216d8fc5c286211545436e4",
		Modulename: "",
		Hasmain:    0,
		Bad:        false,
	})
	mds = append(mds, &runtime.MockModuleData{
		Minpc:      139632062398464,
		Maxpc:      139632062401868,
		Types:      0,
		Etypes:     0,
		Gofunc:     827321229760,
		End:        0,
		Pluginpath: "",
		Modulename: "",
		Hasmain:    0,
		Bad:        false,
	})
	mds = append(mds, &runtime.MockModuleData{
		Minpc:      139632062402560,
		Maxpc:      139632062446597,
		Types:      0,
		Etypes:     0,
		Gofunc:     825732248832,
		End:        0,
		Pluginpath: "",
		Modulename: "",
		Hasmain:    0,
		Bad:        false,
	})
	mds = append(mds, &runtime.MockModuleData{
		Minpc:      139824618976288,
		Maxpc:      139824619868141,
		Types:      139824620072768,
		Etypes:     139824620264408,
		Gofunc:     139824619911344,
		End:        139824623364208,
		Pluginpath: "plugin/unnamed-ccb6042cd7c11889b7579a1e138fb08012f73267",
		Modulename: "",
		Hasmain:    0,
		Bad:        false,
	})
	mds = append(mds, &runtime.MockModuleData{
		Minpc:      139824623961120,
		Maxpc:      139824624852973,
		Types:      139824625057600,
		Etypes:     139824625249240,
		Gofunc:     139824624896176,
		End:        139824628349040,
		Pluginpath: "plugin/unnamed-2fb54c791d7e40a37a6f23a8ba65dd5c2f69912f",
		Modulename: "",
		Hasmain:    0,
		Bad:        false,
	})
	totalModules := 0
	correctModules := 0
	correctPlugins := 0
	updateCount := func(mds []*runtime.MockModuleData) {
		for _, md := range mds {
			totalModules++
			if md.Bad {
				continue
			}
			correctModules++
			if md.Pluginpath != "" || md.Hasmain != 0 {
				correctPlugins++
			}
		}
	}
	updateCount(mds)
	if totalModules == correctModules {
		t.Errorf("total modules %d and correct modules %d are the same", totalModules, correctModules)
	}
	runtime.MockInitModuleDataOpt(mds)
	modules, plugins := runtime.MockGetModuleDataOpt()
	if len(modules) != correctModules {
		t.Errorf("unexpect sorted moduledata want: %d get %d", correctModules, len(modules))
	}
	if len(plugins) != correctPlugins {
		t.Errorf("unexpect sorted plugindata want: %d get %d", correctPlugins, len(plugins))
	}
	checkSortedModules := func(mds []*runtime.MockModuleData) {
		var prev *runtime.MockModuleData
		for _, v := range mds {
			if prev == nil {
				prev = v
			} else {
				if v.Minpc < prev.Maxpc {
					t.Errorf("pc not sorted")
				}
				prev = v
			}
		}
	}
	checkSortedPlugins := func(mds []*runtime.MockModuleData) {
		var prev *runtime.MockModuleData
		for _, v := range mds {
			if prev == nil {
				prev = v
			} else {
				if v.Minpc < prev.Maxpc {
					t.Errorf("pc not sorted")
				}
				if v.Types < prev.Etypes {
					t.Errorf("types not sorted")
				}
				if v.Gofunc < prev.End {
					t.Errorf("gofunc not sorted")
				}
				prev = v
			}
		}
	}
	checkSortedModules(modules)
	checkSortedPlugins(plugins)
	newMds := make([]*runtime.MockModuleData, 0, 10)
	newMds = append(newMds, &runtime.MockModuleData{
		Minpc:      139824589067296,
		Maxpc:      139824589959149,
		Types:      139824590163776,
		Etypes:     139824590355416,
		Gofunc:     139824590002352,
		End:        139824593455216,
		Pluginpath: "plugin/unnamed-1b22d7ccf95a900c206245ed39ab65296f4340de",
		Modulename: "",
		Hasmain:    0,
		Bad:        false,
	})
	newMds = append(newMds, &runtime.MockModuleData{
		Minpc:      139824594052128,
		Maxpc:      139824594943981,
		Types:      139824595148608,
		Etypes:     139824595340248,
		Gofunc:     139824594987184,
		End:        139824598440048,
		Pluginpath: "plugin/unnamed-6f303b693e3a2e5ed394d5ff5b99ae5bf960e67d",
		Modulename: "",
		Hasmain:    0,
		Bad:        false,
	})
	updateCount(newMds)
	runtime.MockInsertModuleDataOpt(newMds)
	modules, plugins = runtime.MockGetModuleDataOpt()
	if len(modules) != correctModules {
		t.Errorf("unexpect sorted moduledata want: %d get %d", correctModules, len(modules))
	}
	if len(plugins) != correctPlugins {
		t.Errorf("unexpect sorted plugindata want: %d get %d", correctPlugins, len(plugins))
	}
	checker := func(md *runtime.MockModuleData, isNil bool, isMain bool, pluginPath string) {
		if isNil {
			if md != nil {
				t.Errorf("unexpect not nil module")
				return
			}
		} else {
			if md == nil {
				t.Errorf("unexpect nil module")
				return
			}
			if isMain {
				if md.Hasmain != 1 {
					t.Errorf("unexpect hasmain %d, want 1", md.Hasmain)
				}
			}
			if pluginPath != md.Pluginpath {
				t.Errorf("unexpect pluginpath %s, want %s", md.Pluginpath, pluginPath)
			}
		}
	}
	allmds := append(mds, newMds...)
	for _, md := range allmds {
		if md.Bad {
			checker(runtime.FindModuledataByPc(md.Minpc-1), true, false, "")
			checker(runtime.FindModuledataByPc(md.Minpc), true, false, "")
			checker(runtime.FindModuledataByPc(md.Minpc+1), true, false, "")
			checker(runtime.FindModuledataByPc(md.Maxpc-1), true, false, "")
			checker(runtime.FindModuledataByPc(md.Maxpc), true, false, "")
			checker(runtime.FindModuledataByPc(md.Maxpc+1), true, false, "")
			continue
		} else {
			checker(runtime.FindModuledataByPc(md.Minpc-1), true, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByPc(md.Minpc), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByPc(md.Minpc+1), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByPc(md.Maxpc-1), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByPc(md.Maxpc), true, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByPc(md.Maxpc+1), true, md.Hasmain == 1, md.Pluginpath)
		}
		if md.Pluginpath != "" {
			checker(runtime.FindModuledataByType(md.Types-1), true, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByType(md.Types), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByType(md.Types+1), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByType(md.Etypes-1), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByType(md.Etypes), true, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByType(md.Etypes+1), true, md.Hasmain == 1, md.Pluginpath)

			checker(runtime.FindModuledataByGCdata(md.Gofunc-1), true, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByGCdata(md.Gofunc), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByGCdata(md.Gofunc+1), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByGCdata(md.End-1), false, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByGCdata(md.End), true, md.Hasmain == 1, md.Pluginpath)
			checker(runtime.FindModuledataByGCdata(md.End+1), true, md.Hasmain == 1, md.Pluginpath)
		}
	}
}

type globalType struct {
	Name string
}

func TestFirstModuleDataFastPath(t *testing.T) {
	if runtime.IsModuleDataOptEnabled() {
		t.Skipf("skipping test due to it will reset ModuleDataOpt data")
	}
	runtime.MockResetModuleDataOpt()
	entry := runtime.FuncForPC(reflect.ValueOf(TestFirstModuleDataFastPath).Pointer()).Entry()
	md := runtime.FindModuledataByPc(entry)
	if md == nil {
		t.Errorf("unexpect nil firstmoduledata")
	}
	var typ interface{} = globalType{}
	rType := runtime.GetStructRtype(&typ)
	md = runtime.FindModuledataByType(rType)
	if md == nil {
		t.Errorf("unexpect nil firstmoduledata")
	}
}
