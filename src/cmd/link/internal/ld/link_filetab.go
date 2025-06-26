// Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build goexperiment.tangollvm && linux && amd64 && cgo && !compiler_bootstrap

package ld

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
)

type FileEntry struct {
	Index uint32
	Dir   string
	File  string
}

func (fe *FileEntry) FullPath() string {
	return filepath.Join(fe.Dir, fe.File)
}

func ReadGoFileTab(reader io.Reader) ([]FileEntry, error) {
	var entryCount uint32
	if err := binary.Read(reader, binary.LittleEndian, &entryCount); err != nil {
		return nil, fmt.Errorf("failed to read entry count: %v", err)
	}

	entries := make([]FileEntry, 0, entryCount)
	for i := uint32(0); i < entryCount; i++ {
		var entry FileEntry
		if err := binary.Read(reader, binary.LittleEndian, &entry.Index); err != nil {
			return nil, fmt.Errorf("failed to read index for entry %d: %v", i, err)
		}

		var dirLen uint32
		if err := binary.Read(reader, binary.LittleEndian, &dirLen); err != nil {
			return nil, fmt.Errorf("failed to read dir length for entry %d: %v", i, err)
		}

		if dirLen > 0 {
			dirBytes := make([]byte, dirLen)
			if _, err := io.ReadFull(reader, dirBytes); err != nil {
				return nil, fmt.Errorf("failed to read dir string for entry %d: %v", i, err)
			}
			entry.Dir = string(dirBytes)
		}

		var fileLen uint32
		if err := binary.Read(reader, binary.LittleEndian, &fileLen); err != nil {
			return nil, fmt.Errorf("failed to read file length for entry %d: %v", i, err)
		}

		if fileLen > 0 {
			fileBytes := make([]byte, fileLen)
			if _, err := io.ReadFull(reader, fileBytes); err != nil {
				return nil, fmt.Errorf("failed to read file string for entry %d: %v", i, err)
			}
			entry.File = string(fileBytes)
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func ReadGoFileTabFromBytes(data []byte) ([]FileEntry, error) {
	reader := bytes.NewReader(data)
	return ReadGoFileTab(reader)
}
