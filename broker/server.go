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

// Package broker implements the cloud broker.
package broker

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"

	"golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	emulators "google/emulators"
	pb "google/protobuf"
)

var (
	EMPTY = &pb.Empty{}
)
var config *Config

type server struct {
	specs            map[string]*emulators.EmulatorSpec
	runningEmulators map[string]*exec.Cmd
	mu               sync.Mutex
}

func New() *server {
	log.Printf("Broker: Server created.")
	return &server{
		specs:            make(map[string]*emulators.EmulatorSpec),
		runningEmulators: make(map[string]*exec.Cmd),
	}
}

// Creates a spec to resolve targets to specified emulator endpoints.
// If a spec with this id already exists, returns ALREADY_EXISTS.
func (s *server) CreateEmulatorSpec(ctx context.Context, req *emulators.CreateEmulatorSpecRequest) (*emulators.EmulatorSpec, error) {
	log.Printf("Broker: CreateEmulatorSpec %v.", req.Spec)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.specs[req.SpecId]
	if ok {
		return nil, grpc.Errorf(codes.AlreadyExists, "Emulator spec %q already exists.", req.SpecId)
	}

	s.specs[req.SpecId] = req.Spec
	return req.Spec, nil
}

// Finds a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) GetEmulatorSpec(ctx context.Context, specId *emulators.SpecId) (*emulators.EmulatorSpec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spec, ok := s.specs[specId.Value]
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, "Emulator spec %q doesn't exist.", specId.Value)
	}
	return spec, nil
}

// Updates a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) UpdateEmulatorSpec(ctx context.Context, spec *emulators.EmulatorSpec) (*emulators.EmulatorSpec, error) {
	log.Printf("Broker: UpdateEmulatorSpec %v.", spec)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.specs[spec.Id]
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, "Emulator spec %q doesn't exist.", spec.Id)
	}
	s.specs[spec.Id] = spec
	return spec, nil
}

// Removes a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) DeleteEmulatorSpec(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.specs[specId.Value]
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, "Emulator spec %q doesn't exist.", specId.Value)
	}
	delete(s.specs, specId.Value)
	return EMPTY, nil
}

// Lists all specs.
func (s *server) ListEmulatorSpecs(ctx context.Context, _ *pb.Empty) (*emulators.ListEmulatorSpecsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var l []*emulators.EmulatorSpec
	for _, v := range s.specs {
		l = append(l, v)
	}
	return &emulators.ListEmulatorSpecsResponse{Specs: l}, nil
}

func outputLogPrefixer(prefix string, in io.Reader) {
	buffReader := bufio.NewReader(in)
	for {
		line, _, err := buffReader.ReadLine()
		if err != nil {
			log.Printf("IO Error %v", err)
			return
		}
		log.Printf("%s: %s", prefix, line)
	}
}

func (s *server) StartEmulator(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	log.Printf("Broker: StartEmulator %v.", specId)

	s.mu.Lock()
	defer s.mu.Unlock()

	id := specId.Value
	_, alreadyRunning := s.runningEmulators[id]
	if alreadyRunning {
		return nil, grpc.Errorf(codes.FailedPrecondition, "Emulator %q is already running.", id)
	}

	cmdLine := s.specs[id].CommandLine
	cmd := exec.Command(cmdLine.Path, cmdLine.Args...)

	// Create stdout, stderr streams of type io.Reader
	pout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	go outputLogPrefixer(specId.Value, pout)

	perr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	go outputLogPrefixer("ERR "+specId.Value, perr)

	if err = cmd.Start(); err != nil {
		return nil, err
	}

	s.runningEmulators[id] = cmd
	return EMPTY, nil
}

func (s *server) StopEmulator(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	log.Printf("Broker: StopEmulator %v.", specId)
	s.mu.Lock()
	defer s.mu.Unlock()
	id := specId.Value
	cmd, running := s.runningEmulators[id]
	if !running {
		return nil, grpc.Errorf(codes.FailedPrecondition, "Emulator %q is not running.", id)
	}
	cmd.Process.Signal(os.Interrupt)
	// TODO: actually check if the process is stopped.
	delete(s.runningEmulators, specId.Value)
	return EMPTY, nil
}

func (s *server) ListEmulators(ctx context.Context, _ *pb.Empty) (*emulators.ListEmulatorsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nil, nil
}

// Resolves a target according to relevant specs. If no spec apply, the input
// target is returned in the response.
func (s *server) Resolve(ctx context.Context, req *emulators.ResolveRequest) (*emulators.ResolveResponse, error) {
	log.Printf("Broker: Resolve target %v.", req.Target)
	s.mu.Lock()
	defer s.mu.Unlock()
	/*	log.Printf("Resolve %q", req)
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
		}*/
	return nil, fmt.Errorf("%s not found", req.Target)
}
