#!/bin/bash
SRC=$GOPATH/src
rm -Rf $SRC/google

echo "gateway go"
protoc \
  google/fakes/gateway.proto \
  -I . --go_out=plugins=grpc:$SRC

echo "protobuf go"
protoc \
  google/protobuf/*.proto \
  -I . --go_out=plugins=grpc:$GOPATH/src

echo "python"
rm -Rf python/google

protoc \
  google/fakes/gateway.proto \
  google/protobuf/*.proto \
-I . --python_out=python --grpc_out=python \
  --plugin=protoc-gen-grpc=`which grpc_python_plugin`

echo "additional python package fixups"
touch python/google/__init__.py
touch python/google/protobuf/__init__.py
touch python/google/fakes/__init__.py

mv python/google python/google_ext

