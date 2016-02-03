#!/bin/bash

# Copyright 2014 Google Inc. All Rights Reserved.
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
# in a Zip archive.

TARGET_PACKAGE=cloud-testenv-broker/cmd/broker
OUTPUT_DIR=build-output
OUTPUT_ZIP="broker.zip"

# Bail on errors.
set -e

rm -rf ${OUTPUT_DIR}/*
mkdir -p build-output/{linux,mac,windows}

echo "Building for Linux..."
env GOOS=linux GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/linux/broker ${TARGET_PACKAGE}
echo "Building for Mac..."
env GOOS=darwin GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/mac/broker ${TARGET_PACKAGE}
echo "Building for Windows..."
env GOOS=windows GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/windows/broker.exe ${TARGET_PACKAGE}

echo "Creating ${OUTPUT_DIR}/${OUTPUT_ZIP} ..."
cd ${OUTPUT_DIR}
zip -r ${OUTPUT_ZIP} linux mac windows
echo "Build successful."
