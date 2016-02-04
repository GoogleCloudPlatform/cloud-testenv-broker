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

BROKER_PACKAGE=cloud-testenv-broker/cmd/broker
LAUNCHER_PACKAGE=cloud-testenv-broker/cmd/launcher
BUILD_DIR=build-output
OUTPUT_DIR="${BUILD_DIR}/broker"
OUTPUT_ZIP="cloud-testenv-broker.zip"

# Bail on errors.
set -e

rm -rf ${OUTPUT_DIR}/*

echo "Building for Linux..."
env GOOS=linux GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/linux/broker ${BROKER_PACKAGE}
env GOOS=linux GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/linux/launcher ${LAUNCHER_PACKAGE}
echo "Building for Mac..."
env GOOS=darwin GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/darwin/broker ${BROKER_PACKAGE}
env GOOS=darwin GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/darwin/launcher ${LAUNCHER_PACKAGE}
echo "Building for Windows..."
env GOOS=windows GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/windows/broker.exe ${BROKER_PACKAGE}
env GOOS=windows GOARCH=amd64 go build -v -o ${OUTPUT_DIR}/windows/launcher.exe ${LAUNCHER_PACKAGE}

echo "Creating ${BUILD_DIR}/${OUTPUT_ZIP} ..."
cd ${BUILD_DIR}
zip -r ${OUTPUT_ZIP} $(basename ${OUTPUT_DIR})
echo "Build successful."
