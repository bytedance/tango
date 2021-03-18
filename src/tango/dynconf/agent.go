// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dynconf provides dynamic configuration mechanism
// for Go program.
// To allow manipulating the running program in following ways:
// - inspect internal status of Go runtime
// - modify Go runtime configurations without restarting the process
// - execute simple runtime tasks, such as runtime.GC
package dynconf

import (
	"fmt"
	"internal/godebug"
	"net/http"
	"runtime"
	"strings"
)

type cmdHandler func([]byte) ([]byte, error)

var (
	commands = map[string]cmdHandler{
		"get":  cmdGetConfig,
		"set":  cmdSetConfig,
		"list": cmdListConfigs,
		"run":  cmdRun,
	}
)

func init() {
	loadRuntimeConfigs()
	if godebug.New("httpmuxgo121").Value() != "1" {
		http.HandleFunc("GET /debug/pprof/dynconf", handleDynconf)
		http.HandleFunc("POST /debug/pprof/dynconf", handleDynconf)
	} else {
		http.HandleFunc("/debug/pprof/dynconf", handleDynconf)
	}
}

func handleDynconf(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	cmd := r.FormValue("cmd")
	task, ok := commands[cmd]
	if !ok {
		serveError(w, http.StatusNotFound, fmt.Sprintf("Invalid command \"%s\"\n", cmd))
		return
	}
	args := r.FormValue("args")
	res, err := task([]byte(args))
	if err != nil {
		serveError(w, http.StatusInternalServerError, "Request failed")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(res)
}

func serveError(w http.ResponseWriter, status int, txt string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Del("Content-Disposition")
	w.WriteHeader(status)
	fmt.Fprintln(w, txt)
}

func cmdSetConfig(args []byte) (res []byte, err error) {
	pairs := strings.Split(string(args), ",")
	for _, pair := range pairs {
		arrs := strings.Split(pair, "=")
		if len(arrs) != 2 || len(arrs[1]) == 0 {
			msg := "empty value to set"
			res = append(res, []byte(msg)...)
			err = fmt.Errorf("%s", msg)
			break
		}
		k := arrs[0]
		v := arrs[1]
		if cfg, ok := dyncfgMap[k]; ok {
			e := cfg.setStringValue(v)
			if e == nil {
				res = append(res, []byte(fmt.Sprintf("Successful: %s=%s\n", k, v))...)
			} else {
				res = append(res, []byte(fmt.Sprintf("Failed: %v\n", e))...)
				err = e
				break
			}
		} else {
			err = fmt.Errorf("config \"%s\" does not exist", k)
			res = append(res, []byte(fmt.Sprintf("Failed: %v\n", err))...)
			break
		}
	}
	return res, err
}

func cmdGetConfig(args []byte) (res []byte, err error) {
	names := strings.Split(string(args), ",")
	for _, name := range names {
		if cfg, ok := dyncfgMap[name]; ok {
			res = append(res, []byte(cfg.String())...)
		} else {
			err = fmt.Errorf("config \"%s\" does not exist", name)
			res = append(res, []byte(fmt.Sprintf("Failed: %v\n", err))...)
			break
		}
	}
	return res, err
}

func cmdListConfigs(args []byte) ([]byte, error) {
	res := []string{}
	for _, cfg := range dyncfgMap {
		res = append(res, cfg.String())
	}
	for t := range taskMap {
		res = append(res, "task: "+t)
	}
	return []byte(strings.Join(res, "\n")), nil
}

// tasks for cmd=run
var taskMap = map[string]func() ([]byte, error){
	"runtime.GC": func() ([]byte, error) {
		runtime.GC()
		return nil, nil
	},
}

func cmdRun(args []byte) (res []byte, err error) {
	// support executing multiple commands sequentially,
	// but will cease running if any task reports an error
	cmds := strings.Split(string(args), ",")
	for _, cmd := range cmds {
		if task, ok := taskMap[cmd]; ok {
			data, e := task()
			if len(data) > 0 {
				res = append(res, data...)
				res = append(res, []byte("\n")...)
			}
			if e != nil {
				err = e
				res = append(res, []byte(fmt.Sprintf("error %v\nFailed: %s", e, cmd))...)
				break
			}
			res = append(res, []byte(fmt.Sprintf("Successful: %s\n", cmd))...)
		} else {
			err = fmt.Errorf("task not found: %s", cmd)
			res = append(res, []byte(fmt.Sprintf("error %v\nFailed: %s", err, cmd))...)
			break
		}
	}
	return res, err
}
