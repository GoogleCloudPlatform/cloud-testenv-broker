# Cloud Testing Environment Broker

This is a discovery and lifecycle tool to create a local testing environment of
gRPC-based emulators.

## Prerequisite:

Have a working golang environment see [Go
environment](https://golang.org/doc/code.html)

## Dependencies:

Latest and greatest from:

- http://www.github.com/google/protobuf
- http://www.github.com/google/grpc
- http://www.github.com/grpc-ecosystem/grpc-gateway

ie.

```shell
go get -u github.com/bradfitz/http2
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/grpc-ecosystem/grpc-gateway/runtime
go get -u github.com/golang/glog
go get -u github.com/golang/protobuf/protoc-gen-go
go get -u google.golang.org/grpc
```

## SetupQuick instructions

```shell
# From your Go tree.
mkdir -p $GOPATH/src/github.com/GoogleCloudPlatform
cd $GOPATH/src/github.com/GoogleCloudPlatform

# Clone the main project
git clone https://github.com/GoogleCloudPlatform/cloud-testenv-broker.git

# Update the submodules
cd cloud-testenv-broker
git submodule init
git submodule update

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
