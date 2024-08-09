// Copyright 2024 ByteDance Ltd. and/or its affiliates. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

func (c *DiskCache) CTGetDirHashFileName(id ActionID) string {
	return c.fileName(id, "a")
}
