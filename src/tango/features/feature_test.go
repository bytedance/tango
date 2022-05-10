// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package features

import (
	"fmt"
	"math"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	instance := getTangoFeatures()
	if feature, ok := instance.(tangoFeatures); !ok {
		t.Fatal("tangoInstance not implement tangoFeatures interface")
	} else {
		version := feature.Version()
		bVersion := feature.BVersion()
		versionPattern := "go[123]\\.\\d+\\.?(rc\\d)?((\\d{1,2}\\.))*"
		if matched, err := regexp.MatchString(versionPattern, version); err == nil {
			if !matched {
				t.Errorf("Version %s does not match pattern %s\n", version, versionPattern)
			}
		} else {
			t.Errorf("Error matching %s to pattern %s", version, versionPattern)
		}

		bVersionPattern := "(devel )*go[123]\\.\\d+(rc\\d)?\\.((\\d{1,2}\\.))*bm\\.\\d\\.\\d\\..*"
		if matched, err := regexp.MatchString(bVersionPattern, bVersion); err == nil {
			if !matched {
				t.Errorf("BVersion %s does not match pattern %s\n", bVersion, bVersionPattern)
			}
		} else {
			t.Errorf("Error matching %s to pattern %s", bVersion, bVersionPattern)
		}
	}
}

func TestFeatureCount(t *testing.T) {
	instance := getTangoFeatures()
	if feature, ok := instance.(tangoFeatures); !ok {
		t.Error("tangoInstance not implement tangoFeatures interface")
	} else {
		allFeatures := feature.AllFeatures()
		if len(allFeatures) != featureCount {
			t.Error("len of allFeatures not equal to featureCount")
		}
	}
}

func TestAllFeatures(t *testing.T) {
	instance := getTangoFeatures()
	if tangofeature, ok := instance.(tangoFeatures); !ok {
		t.Error("tangoInstance not implement tangoFeatures interface")
	} else {
		allFeatures := tangofeature.AllFeatures()
		for feaName, feaInterface := range allFeatures {
			if fea, ok := feaInterface.(feature); !ok {
				t.Error("featureInstance not implement feature interface")
			} else {
				if name := fea.Name(); name == "" || name != feaName {
					t.Errorf("Feature %s name is wrong, get %s", feaName, name)
				}
				if ver := fea.Version(); ver == "" {
					t.Errorf("Feature %s version is wrong, get %s", feaName, ver)
				}
				// Test not panic.
				fea.Description()
				fea.Enabled()
				fea.SetAttr("", nil)
				fea.SetAttr("XXX", nil)
				fea.SetAttr(attrMode, nil)
				fea.SetAttr(attrDetail, nil)
				fea.SetAttr(attrHeapLimit, nil)

				fea.GetAttr("")
				fea.GetAttr("XXX")
				fea.GetAttr(attrMode)
				fea.GetAttr(attrDetail)
				fea.GetAttr(attrHeapLimit)
			}
		}
	}
}

func TestFeaturesDetailInfo(t *testing.T) {
	instance := getTangoFeatures()
	if tangofeature, ok := instance.(tangoFeatures); !ok {
		t.Error("tangoInstance not implement tangoFeatures interface")
	} else {
		allFeatures := tangofeature.AllFeatures()
		if beastInterface, ok := allFeatures[featureBeastmode]; ok {
			if beast, ok := beastInterface.(feature); ok {
				detail := beast.GetAttr(attrMode)
				if _, ok := detail.(string); !ok {
					t.Errorf("Feature %s GetAttr[%s] return wrong type", beast.Name(), attrMode)
				}
				if detail.(string) == "" {
					t.Errorf("Feature %s GetAttr[%s] return wrong value, get %s", beast.Name(), attrMode, detail)
				}
			}
		}

		if adaptSchedInterface, ok := allFeatures[featureAdaptsched]; ok {
			if beast, ok := adaptSchedInterface.(feature); ok {
				detail := beast.GetAttr(attrDetail)
				if _, ok := detail.(string); !ok {
					t.Errorf("Feature %s GetAttr[%s] return wrong type", beast.Name(), attrDetail)
				}
				if detail.(string) == "" {
					t.Errorf("Feature %s GetAttr[%s] return wrong value, get %s", beast.Name(), attrDetail, detail)
				}
			}
		}
	}
}

func TestFeaturesMaxheap(t *testing.T) {
	instance := getTangoFeatures()
	if tangofeature, ok := instance.(tangoFeatures); !ok {
		t.Error("tangoInstance not implement tangoFeatures interface")
	} else {
		allFeatures := tangofeature.AllFeatures()
		if maxHeapInterface, ok := allFeatures[featureMaxheap]; ok {
			if maxHeap, ok := maxHeapInterface.(feature); ok {
				defer maxHeap.SetAttr(attrHeapLimit, math.MaxInt64)

				err := maxHeap.SetAttr(attrHeapLimit, nil)
				if err != errWrongArgsType {
					t.Errorf("Feature %s SetAttr[%s] return error is not as expected, get \"%s\", want \"%s\"", maxHeap.Name(), attrHeapLimit, err, errWrongArgsType)
				}

				err = maxHeap.SetAttr(attrHeapLimit, "XXX")
				if err != errWrongArgsType {
					t.Errorf("Feature %s SetAttr[%s] return error is not as expected, get \"%s\", want \"%s\"", maxHeap.Name(), attrHeapLimit, err, errWrongArgsType)
				}

				err = maxHeap.SetAttr(attrDetail, "XXX")
				if !strings.Contains(err.Error(), "not configurable on feature") {
					t.Errorf("Feature %s SetAttr[%s] return error is not as expected, get \"%s\", want \"%s\"", maxHeap.Name(), attrHeapLimit, err, "not configurable")
				}

				err = maxHeap.SetAttr(attrHeapLimit, int64(10*1024*1024))
				if err != nil {
					t.Errorf("Feature %s SetAttr[%s] return error \"%s\"", maxHeap.Name(), attrHeapLimit, err)
				}
				runtime.GC()

				currMaxHeap := maxHeap.GetAttr(attrHeapLimit)
				if value, ok := currMaxHeap.(int64); !ok {
					t.Errorf("Feature %s GetAttr[%s] return wrong type", maxHeap.Name(), attrHeapLimit)
				} else {
					if value != 10*1024*1024 {
						t.Errorf("Feature %s GetAttr[%s] return wrong value, get %d, want %d", maxHeap.Name(), attrHeapLimit, value, 10*1024*1024)
					}
				}

				err = maxHeap.SetAttr(attrHeapLimit, int64(20*1024*1024))
				if err != nil {
					t.Errorf("Feature %s SetAttr[%s] return error \"%s\"", maxHeap.Name(), attrHeapLimit, err)
				}
				runtime.GC()
				currMaxHeap = maxHeap.GetAttr(attrHeapLimit)
				if value, ok := currMaxHeap.(int64); !ok {
					t.Errorf("Feature %s GetAttr[%s] return wrong type", maxHeap.Name(), attrHeapLimit)
				} else {
					if value != 20*1024*1024 {
						t.Errorf("Feature %s GetAttr[%s] return wrong value, get %d, want %d", maxHeap.Name(), attrHeapLimit, value, 20*1024*1024)
					}
				}
			}
		}
	}
}

type versionInfo struct {
	Name    string
	Version string
}

// Get version information of all features, used in version release.
// go test -v -run=^TestGetAllFeaturesVersion$ tango/features |grep -v "RUN" |grep -v "PASS" |grep -v "ok"
func TestGetAllFeaturesVersion(t *testing.T) {
	allVersion := make([]versionInfo, 0, featureCount)
	instance := getTangoFeatures()
	if tangofeature, ok := instance.(tangoFeatures); !ok {
		t.Error("tangoInstance not implement tangoFeatures interface")
	} else {
		allFeatures := tangofeature.AllFeatures()
		for _, feaInterface := range allFeatures {
			if fea, ok := feaInterface.(feature); !ok {
				t.Error("featureInstance not implement feature interface")
			} else {
				allVersion = append(allVersion, versionInfo{
					Name:    fea.Name(),
					Version: fea.Version(),
				})
			}
		}
	}
	sort.Slice(allVersion, func(i, j int) bool { return allVersion[i].Name < allVersion[j].Name })
	res := strings.Builder{}
	for _, v := range allVersion {
		res.WriteString(fmt.Sprintf("%s(%s);", v.Name, v.Version))
	}
	fmt.Println(res.String())
}
