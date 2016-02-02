#!/bin/sh

# Builds the broker binary for Linux, Mac, and Windows, and packages all
# in a Zip archive.

TARGET_PACKAGE=cloud-testenv-broker/broker/main
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
