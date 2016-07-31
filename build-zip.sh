#!/bin/bash

# Copyright 2016 Google Inc. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Builds the broker binary for Linux, Mac, and Windows, and packages all
# in Zip archives.

PROJECT_DIR=github.com/GoogleCloudPlatform/cloud-testenv-broker
BROKER_PACKAGE=$PROJECT_DIR/cmd/broker
LAUNCHER_PACKAGE=$PROJECT_DIR/cmd/launcher
BUILD_DIR=build-output
ARCHIVE_NAME=broker

TARGET_OS="linux windows darwin"
TARGET_ARCH="amd64 386"

rm -rf ${BUILD_DIR}/*
mkdir ${BUILD_DIR}
cd ${BUILD_DIR}

# Bail on errors.
set -e

for os in ${TARGET_OS}; do
  for arch in ${TARGET_ARCH}; do
    echo "Building for ${os}/${arch}..."
    ext=""
    if [ "${os}" == "windows" ]; then
      ext=".exe"
    fi
    env GOOS=${os} GOARCH=${arch} go build -o ${ARCHIVE_NAME}/broker${ext} ${BROKER_PACKAGE}
    env GOOS=${os} GOARCH=${arch} go build -o ${ARCHIVE_NAME}/launcher${ext} ${LAUNCHER_PACKAGE}

    archive_file="${ARCHIVE_NAME}_${os}_${arch}.zip"
    echo "Creating ${archive_file} ..."
    zip -r ${archive_file} ${ARCHIVE_NAME}
    rm -rf ${ARCHIVE_NAME}
  done
done

echo "Build successful."
