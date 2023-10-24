// Copyright 2021 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package main
package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime/debug"
	"strconv"
	"time"
)

var (
	port    int
	timeout int
)

func init() {
	flag.IntVar(&port, "p", 0, "HTTP port to listen on")
	flag.IntVar(&timeout, "t", 10, "forceful timeout period in second")
	flag.Parse()
}

func main() {
	if port == 0 {
		flag.PrintDefaults()
		os.Exit(1)
	}
	http.HandleFunc("/debug/kill", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Ended by /debug/kill\n")
		os.Exit(0)
	})
	http.HandleFunc("/debug/setMemoryLimit", func(w http.ResponseWriter, r *http.Request) {
		value := r.FormValue("value")
		if value == "" {
			return
		}
		if i64, err := strconv.ParseInt(value, 10, 0); err == nil {
			debug.SetMemoryLimit(i64)
		}
	})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fmt.Printf("Listening on %s\n", addr)
	go http.ListenAndServe(addr, nil)

	ts := time.Now()
	remain := time.Duration(timeout) * time.Second
	for {
		if remain > 0 {
			time.Sleep(remain)
		}
		elapsed := time.Now().Sub(ts)
		if elapsed <= remain {
			remain -= elapsed
		} else {
			fmt.Printf("Ended after forceful timeout %ds\n", timeout)
			break
		}
	}
}
