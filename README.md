# Cloud Testing Environment Broker

This is a discovery and lifecycle tool to create a local testing environment of
grpc-based emulators.

## Prerequisite:

Have a working golang environment see [Go
environment](https://golang.org/doc/code.html)

## Dependencies:

Latest and greatest from:

- http://www.github.com/google/protobuf
- http://www.github.com/google/grpc

ie.

```shell
go get -u google.golang.org/grpc
go get -u github.com/golang/protobuf/protoc-gen-go
```


## SetupQuick instructions

```shell
# From your Go tree.
cd $GOPATH/src

# Clone the main project
git clone https://github.com/GoogleCloudPlatform/cloud-testenv-broker.git

# Update the submodules
cd cloud-testenv-broker
git submodule init
git submodule update

# Generate the source code from the proto files
./gen-proto.sh

# (you can find the generated files in $GOPATH/src/google)

# You can test your enviroenemnt by running the broker in standalone mode
./run-broker.sh

```
