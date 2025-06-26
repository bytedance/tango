#!/usr/bin/env bash
# Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.
set -euo pipefail
: "${TANGOLLVM_REPO:?Set TANGOLLVM_REPO to the LLVM source checkout}"
export TANGO_ROOT
TANGO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
exec "$TANGOLLVM_REPO/tango/build.sh" "$@"
