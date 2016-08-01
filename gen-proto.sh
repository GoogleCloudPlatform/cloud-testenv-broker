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
PTYPES=github.com/golang/protobuf/ptypes
GOOGLEAPIS=github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis
PKGMAP=Mgoogle/protobuf/duration.proto=$PTYPES/duration,Mgoogle/protobuf/empty.proto=$PTYPES/empty,Mgoogle/api/annotations.proto=$GOOGLEAPIS/google/api

rm -f $SRC/google/emulators/broker.*

echo "GO: broker protos"
protoc -I protos \
   -I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
  --go_out=$PKGMAP,plugins=grpc:$SRC \
  --grpc-gateway_out=$PKGMAP,logtostderr=true:$SRC \
  protos/google/emulators/broker.proto
