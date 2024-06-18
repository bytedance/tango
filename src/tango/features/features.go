// Copyright 2022 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package features implements a way to export tango features,
// Specifically, it includes obtaining which features the current go version supports,
// the version number of the feature, and some set or get methods for specific features.
//

package features

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	_ "tango/dynconf"
	_ "unsafe"
)

var featuresVersion map[string]string

const (
	featureBalancedgc = "balancedgc"
	featureMaxheap    = "maxheap"
	featureGCAdaptive = "gcadaptive"
	featureGab        = "gab"
	featureBeastmode  = "beastmode"
	featureAdaptsched = "adaptsched"
	featureDynconf    = "pprof/dynconf"
	featureMemstats   = "pprof/memstats"
	featureBversion   = "bversion"
	featureTangoinfo  = "tangoinfo"
	featureCount      = iota
)

const (
	// The mode attribute return the mode of the feature, currently only used in beast.
	attrMode = "mode"
	// The detail attribute return general status info, this information can be returned according to a custom protocol,
	// Make sure that it can be decoded correctly in the TangoFeature library.
	attrDetail = "detail"
	// The heapLimit attribute is used to set and get the current heap limit value of the featureMaxheap.
	attrHeapLimit = "heapLimit"

	// The following attributes are used in dynconf.
	// The register attribute return a function that can be used to register configuration items.
	attrDynconfRegister = "register"
	// The register attribute return a function that can be used to get configuration items.
	attrDynconfCmdGet = "cmdGet"
	// The register attribute return a function that can be used to set configuration items.
	attrDynconfCmdSet = "cmdSet"
	// The register attribute return a function that can be used to list configuration items.
	attrDynconfCmdList = "cmdList"
)

var (
	errWrongArgsType = errors.New("attribute arguments type incorrect")
)

func initFeatures() {
	featuresVersion = make(map[string]string, featureCount)
	featuresVersion[featureBalancedgc] = "1.0"
	featuresVersion[featureMaxheap] = "1.0"
	featuresVersion[featureGCAdaptive] = "1.0"
	featuresVersion[featureGab] = "1.1"
	featuresVersion[featureBeastmode] = "1.3"
	featuresVersion[featureAdaptsched] = "1.0"
	featuresVersion[featureDynconf] = "1.3"
	featuresVersion[featureMemstats] = "1.0"
	featuresVersion[featureBversion] = "1.1"
	featuresVersion[featureTangoinfo] = "1.1"
}

type tangoFeatures interface {
	Version() string
	BVersion() string
	AllFeatures() map[string]interface{}
}

// This function is a reserved method for 1.19 and later versions,
// We intend to use linkname to provide the method for third-party libraries.
// So please make sure that this method has been added in subsequent major releases.
func getTangoFeatures() interface{} {
	return &tangoInstance{}
}

// The tangoInstance type name is known in the third-party libraries, as a bridge to export tangofeature,
// please do not modify the current package name and the type name.
type tangoInstance struct {
	once        sync.Once
	allFeatures map[string]interface{}
}

func (t *tangoInstance) Version() string {
	return runtime.Version()
}

func (t *tangoInstance) BVersion() string {
	return runtime.BVersion()
}

func (t *tangoInstance) AllFeatures() map[string]interface{} {
	t.once.Do(func() {
		initFeatures()
		t.allFeatures = map[string]interface{}{
			featureBalancedgc: newFeatureInstance(featureBalancedgc, func() bool {
				return isBalanceGcEnabled()
			}),
			featureMaxheap: newMaxHeapInstance(),
			featureGCAdaptive: newFeatureInstance(featureGCAdaptive, func() bool {
				return isGCAdaptiveEnabled()
			}),
			featureGab: newFeatureInstance(featureGab, func() bool {
				return isGabEnabled()
			}),
			featureBeastmode:  newBeastmodeInstance(),
			featureAdaptsched: newAdaptSchedInstance(),
			featureDynconf:    newDynconfInstance(),
			featureMemstats: newFeatureInstance(featureMemstats, func() bool {
				return true
			}),
			featureBversion: newFeatureInstance(featureBversion, func() bool {
				return true
			}),
			featureTangoinfo: newFeatureInstance(featureTangoinfo, func() bool {
				return true
			}),
		}
	})
	return t.allFeatures
}

type feature interface {
	Name() string
	Version() string
	Enabled() bool

	// Description return the introduction of feature, not used now.
	Description() string
	// Generic methods for Get/Set attribute.
	SetAttr(name string, val interface{}) error
	GetAttr(name string) interface{}
}

type featureInstance struct {
	name    string
	version string
	enabled func() bool
}

func newFeatureInstance(name string, enabled func() bool) *featureInstance {
	return &featureInstance{
		name:    name,
		version: featuresVersion[name],
		enabled: enabled,
	}
}

func (f *featureInstance) Name() string {
	return f.name
}

func (f *featureInstance) Version() string {
	return f.version
}

func (f *featureInstance) Enabled() bool {
	if f.enabled == nil {
		return false
	}
	return f.enabled()
}

func (f *featureInstance) Description() string {
	return ""
}

func (f *featureInstance) SetAttr(name string, val interface{}) error {
	return fmt.Errorf("attribute '%s' not configurable on feature '%s'", name, f.name)
}

func (f *featureInstance) GetAttr(name string) interface{} {
	return nil
}

type beastmodeFeature struct {
	*featureInstance
}

func (f *beastmodeFeature) GetAttr(name string) interface{} {
	switch name {
	case attrMode:
		return getBeastModeInfo()
	default:
		return nil
	}
}

func newBeastmodeInstance() feature {
	return &beastmodeFeature{newFeatureInstance(featureBeastmode, isBeastModeEnabled)}
}

type adaptSchedFeature struct {
	*featureInstance
}

func (f *adaptSchedFeature) GetAttr(name string) interface{} {
	switch name {
	case attrDetail:
		return getAdaptSchedOptsInfo()
	default:
		return nil
	}
}

func newAdaptSchedInstance() feature {
	return &adaptSchedFeature{newFeatureInstance(featureAdaptsched, isAdaptSchedEnabled)}
}

type maxHeapFeature struct {
	*featureInstance
}

func (f *maxHeapFeature) SetAttr(name string, val interface{}) error {
	switch name {
	case attrHeapLimit:
		if limit, ok := val.(int64); ok {
			gcSetMaxHeap(limit)
			return nil
		}
		return errWrongArgsType
	default:
		return fmt.Errorf("attribute '%s' not configurable on feature '%s'", name, f.name)
	}
}

func (f *maxHeapFeature) GetAttr(name string) interface{} {
	switch name {
	case attrHeapLimit:
		return gcSetMaxHeap(-1)
	default:
		return nil
	}
}

func newMaxHeapInstance() feature {
	return &maxHeapFeature{newFeatureInstance(featureMaxheap, setMaxHeapEnabled)}
}

type dynconfFeature struct {
	*featureInstance
}

//go:linkname registerDynconf tango/dynconf.registerDynconf
func registerDynconf(namespace, key string, value any, writable bool) error

//go:linkname cmdGetConfig tango/dynconf.cmdGetConfig
func cmdGetConfig(args []byte) (res []byte, err error)

//go:linkname cmdSetConfig tango/dynconf.cmdSetConfig
func cmdSetConfig(args []byte) ([]byte, error)

//go:linkname cmdListConfigs tango/dynconf.cmdListConfigs
func cmdListConfigs(args []byte) ([]byte, error)

func (f *dynconfFeature) GetAttr(name string) interface{} {
	switch name {
	case attrDynconfRegister:
		return registerDynconf
	case attrDynconfCmdGet:
		return cmdGetConfig
	case attrDynconfCmdSet:
		return cmdSetConfig
	case attrDynconfCmdList:
		return cmdListConfigs
	default:
		return nil
	}
}

func newDynconfInstance() feature {
	return &dynconfFeature{newFeatureInstance(featureDynconf, func() bool {
		return true
	})}
}

var global interface{}

func init() {
	// keep it alive.
	global = tangoInstance{}
}

var (
	_ tangoFeatures = &tangoInstance{}   // just a check
	_ feature       = &featureInstance{} // just a check
)
