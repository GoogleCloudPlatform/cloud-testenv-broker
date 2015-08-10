/*
Copyright 2014 Google Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package main implements the fake gateway.
package main

import (
	"flag"
	"fmt"
	"google.golang.org/grpc"
	"log"
	"net"
	re "regexp"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	emulators "google/emulators"
	pb "google/protobuf"
)

var (
	tls        = flag.Bool("tls", false, "Connection uses TLS if true, else plain TCP")
	certFile   = flag.String("cert_file", "server1.pem", "The TLS cert file")
	keyFile    = flag.String("key_file", "server1.key", "The TLS key file")
	port       = flag.Int("port", 10000, "The server port")
	configFile = flag.String("config_file", "", "The json config file of the Gatemay.")
	EMPTY      = &pb.Empty{}
)
var config *Config

type server struct{}

type cmdSpec struct {
	regexp string
	path   string
	args   []string
}

type matcher struct {
	regexps []string
	target  string
}

// This maps the url patterns to targets urls.
// this is a list as the evaluation order matters.
var activeFakes []matcher

// This maps the url patterns to cmd to start to have the fake
var ondemandFakes []cmdSpec

func init() {
	activeFakes = make([]matcher, 0, 10)
	ondemandFakes = make([]cmdSpec, 0, 10)
}

// Creates a spec to resolve targets to specified emulator endpoints.
// If a spec with this id already exists, returns ALREADY_EXISTS.
func (s *server) CreateEmulatorSpec(ctx context.Context, req *emulators.CreateEmulatorSpecRequest) (*emulators.EmulatorSpec, error) {
	log.Printf("Register req %q", req)
	if req.Spec.ResolvedTarget != "" {
		activeFakes = append(activeFakes, matcher{
			regexps: req.Spec.TargetPattern,
			target:  req.Spec.ResolvedTarget,
		})
	} else {
		log.Printf("TODO: implement")
	}
	return &emulators.EmulatorSpec{}, nil
}

// Finds a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) GetEmulatorSpec(ctx context.Context, specId *emulators.SpecId) (*emulators.EmulatorSpec, error) {
	return nil, nil
}

// Updates a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) UpdateEmulatorSpec(ctx context.Context, spec *emulators.EmulatorSpec) (*emulators.EmulatorSpec, error) {
	return nil, nil
}

// Removes a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) DeleteEmulatorSpec(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	return nil, nil
}

// Lists all specs.
func (s *server) ListEmulatorSpecs(ctx context.Context, _ *pb.Empty) (*emulators.ListEmulatorSpecsResponse, error) {
	return nil, nil
}

func (s *server) StartEmulator(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	return nil, nil
}

func (s *server) StopEmulator(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	return nil, nil
}

func (s *server) ListEmulators(ctx context.Context, _ *pb.Empty) (*emulators.ListEmulatorsResponse, error) {
	return nil, nil
}

// Resolves a target according to relevant specs. If no spec apply, the input
// target is returned in the response.
func (s *server) Resolve(ctx context.Context, req *emulators.ResolveRequest) (*emulators.ResolveResponse, error) {
	log.Printf("Resolve %q", req)
	target := []byte(req.Target)
	for _, matcher := range activeFakes {
		for _, regexp := range matcher.regexps {
			matched, err := re.Match(regexp, target)
			if err != nil {
				return nil, err
			}
			if matched {
				res := &emulators.ResolveResponse{
					Target: matcher.target,
				}
				return res, nil
			}
		}
	}
	return nil, fmt.Errorf("%s not found", req.Target)
}

func main() {
	log.Printf("Fakes Gateway starting up...")
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v.", err)
	}
	if *configFile != "" {
		config, err = Decode(*configFile)
		if err != nil {
			log.Fatalf("Could not parse config file: %v", err)
		}
	}
	grpcServer := grpc.NewServer()
	server := server{}
	emulators.RegisterBrokerServer(grpcServer, &server)
	if *tls {
		creds, err := credentials.NewServerTLSFromFile(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("Failed to generate credentials %v.", err)
		}
		log.Printf("Gateway listening with TLS on :%d.", *port)
		grpcServer.Serve(creds.NewListener(lis))
	} else {
		log.Printf("Gateway listening on :%d.", *port)
		grpcServer.Serve(lis)
	}
}
