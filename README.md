# Cloud Testing Environment Broker

This is a discovery and lifecycle tool to create a local testing environment of
gRPC-based emulators.

## Prerequisite:

- Have a working [Go 1.5+ environment](https://golang.org/doc/code.html)
  environment.
- Install [protoc 3.0.0-beta-3 or later]
  (https://github.com/google/protobuf/releases). Ensure the contents of the
  `include` directory is installed.

## Installation

```shell
# Install all dependencies:
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/grpc-ecosystem/grpc-gateway/runtime
go get -u github.com/golang/glog
go get -u github.com/golang/protobuf/protoc-gen-go
go get -u github.com/golang/protobuf/ptypes
go get -u golang.org/x/net/http2
go get -u golang.org/x/net/http2/hpack
go get -u google.golang.org/grpc

mkdir -p $GOPATH/src/github.com/GoogleCloudPlatform
cd $GOPATH/src/github.com/GoogleCloudPlatform

# Clone the main project
git clone https://github.com/GoogleCloudPlatform/cloud-testenv-broker.git
cd cloud-testenv-broker

# Generate the source code from the proto files
# (you can find the generated files in $GOPATH/src/google)
./gen-proto.sh

# Run all tests
go test -v ./...

# Run the broker in standalone mode
./run-broker.sh

# Build a binary distribution for Linux, Mac, and Windows
./build-zip.sh
```
