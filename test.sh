#!/usr/bin/env bash
# Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

TANGO_WORKSPACE="$(cd "$(dirname "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
cd "${TANGO_WORKSPACE}" || exit 1

# update PATH to include the newly built `go`
export PATH="${TANGO_WORKSPACE}/bin:$PATH"
# let the go command to locate GOROOT
export GOROOT=

FAILED=0
pushd test &> /dev/null
bash -x ./test.sh 3 3 1  #llvm=3,optimize=3,inline=1
if [[ $? != 0 ]]; then
    FAILED=1
fi
popd &> /dev/null

# TODO: adapt all tests in test/llvm to testdir
pushd test/llvm &> /dev/null
bash -x ./test.sh 3 3 1  #llvm=3,optimize=3,inline=1
if [[ $? != 0 ]]; then
    FAILED=1
fi
popd &> /dev/null

#pushd src &> /dev/null
#bash -x ./test.sh 3 3 1  #llvm=3,optimize=3,inline=1
#if [[ $? != 0 ]]; then
#    FAILED=1
#fi
#popd &> /dev/null

echo "*******************************************************"
echo "******************** R E S U L T S ********************"
echo "*******************************************************"

echo "====================> test/llvm/failures.txt"
cat test/llvm/failures.txt
rm -f test/llvm/failures.txt

echo "====================> test/failures.txt"
cat test/failures.txt
rm -f test/failures.txt

echo "exit $FAILED"
exit $FAILED
