#!/bin/bash
# Copyright 2025 ByteDance Ltd. and/or its affiliates. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

DIR=$(cd "$(dirname "$0")";pwd)


GCFLAGS="-beast=llvm=1 -d panic=1"
TESTS=( atomic \
    reflect \
)

if [[ "$1" == "clean" ]]; then
    rm -rf ${DIR}/llvm.o
    rm -rf ${DIR}/llvm.ll
    rm -rf ${DIR}/ssa.html
    for T in ${TESTS[@]}
    do
        TBIN_PATH=$DIR/$T
        # // remove old bin
        rm -rf $TBIN_PATH
    done
    exit 0
fi

# uncomment if u want to run `slice' only
#TESTS=(slice)


GO_BIN=${DIR}/../../../bin/go

RESULTS=()

HAS_FAILED=0

for T in ${TESTS[@]}
do
    TBIN_PATH=$DIR/$T
    # // remove old bin
    rm -f $TBIN_PATH
    go clean -cache -testcache

    echo "Building tests $T"
    GOEXPERIMENT=tangollvm $GO_BIN build -o $TBIN_PATH -gcflags "$GCFLAGS" $DIR/$T.go
    if (( $? != 0 )); then
        echo "Failed to build test $T"
        RESULTS+=("$T ...failed to build")
        HAS_FAILED=1
        continue
    fi

    GOGC=off GODEBUG=asyncpreemptoff=1 $TBIN_PATH
    if (( $? != 0 )); then
        echo "Failed to run test $T"
        RESULTS+=("$T ...failed")
        HAS_FAILED=1
        continue
    fi

    RESULTS+=("$T ...passed")
done

echo "============="
for R in "${RESULTS[@]}"
do
    echo $R
done
echo "============="

exit $HAS_FAILED
