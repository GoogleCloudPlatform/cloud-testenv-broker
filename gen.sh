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

