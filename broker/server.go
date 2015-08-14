/*
Copyright 2015 Google Inc. All Rights Reserved.

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
	"net"
	"os"
	"os/exec"
	re "regexp"
	"sync"
	"time"

	"golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	emulators "google/emulators"
	pb "google/protobuf"
)

var (
	EmptyPb = &pb.Empty{}
)

const (
	// emulator states
	StateOffline  = "offline"
	StateStarting = "starting"
	StateOnline   = "online"
)

var config *Config

type emulator struct {
	spec  *emulators.EmulatorSpec
	cmd   *exec.Cmd
	state string
}

func newEmulator(spec *emulators.EmulatorSpec) *emulator {
	return &emulator{spec: spec, state: StateOffline}
}

func (emu *emulator) start() error {
	if emu.state != StateOffline {
		return fmt.Errorf("Emulator %q cannot be started because it is in state %q.", emu.spec.Id, emu.state)
	}

	cmdLine := emu.spec.CommandLine
	cmd := exec.Command(cmdLine.Path, cmdLine.Args...)

	// Create stdout, stderr streams of type io.Reader
	pout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	go outputLogPrefixer(emu.spec.Id, pout)

	perr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	go outputLogPrefixer("ERR "+emu.spec.Id, perr)
	emu.cmd = cmd
	emu.state = StateStarting

	log.Printf("Starting %q", emu.spec.Id)

	err = StartProcessTree(emu.cmd)
	if err != nil {
		log.Printf("Error starting %q", emu.spec.Id)
	}
	return nil
}

func (emu *emulator) markOnline() error {
	if emu.state != StateStarting {
		return fmt.Errorf("Emulator %q cannot be marked online: %s", emu.spec.Id, emu.state)
	}
	emu.state = StateOnline
	return nil
}

func (emu *emulator) kill() error {
	if emu.state == StateOffline {
		log.Printf("Emulator %q cannot be killed because it is not running", emu.spec.Id)
		return nil
	}
	err := KillProcessTree(emu.cmd)
	emu.state = StateOffline
	return err
}

type server struct {
	emulators     map[string]*emulator
	startDeadline time.Duration
	mu            sync.Mutex
}

func New() *server {
	log.Printf("Server created.")
	return &server{emulators: make(map[string]*emulator), startDeadline: time.Minute}
}

// Cleans up this instance, namely its emulators map, killing any that are running.
func (s *server) Clear() {
	s.mu.Lock()
	for _, emu := range s.emulators {
		emu.kill()
	}
	s.emulators = make(map[string]*emulator)
	s.mu.Unlock()
}

// Creates a spec to resolve targets to specified emulator endpoints.
// If a spec with this id already exists, returns ALREADY_EXISTS.
func (s *server) CreateEmulatorSpec(ctx context.Context, req *emulators.CreateEmulatorSpecRequest) (*emulators.EmulatorSpec, error) {
	log.Printf("CreateEmulatorSpec %v.", req.Spec)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.emulators[req.SpecId]
	if ok {
		return nil, grpc.Errorf(codes.AlreadyExists, "Emulator spec %q already exists.", req.SpecId)
	}

	s.emulators[req.SpecId] = newEmulator(req.Spec)
	return req.Spec, nil
}

// Finds a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) GetEmulatorSpec(ctx context.Context, specId *emulators.SpecId) (*emulators.EmulatorSpec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	emu, ok := s.emulators[specId.Value]
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, "Emulator spec %q doesn't exist.", specId.Value)
	}
	return emu.spec, nil
}

// Updates a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) UpdateEmulatorSpec(ctx context.Context, spec *emulators.EmulatorSpec) (*emulators.EmulatorSpec, error) {
	log.Printf("UpdateEmulatorSpec %v.", spec)
	s.mu.Lock()
	defer s.mu.Unlock()
	emu, ok := s.emulators[spec.Id]
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, "Emulator spec %q doesn't exist.", spec.Id)
	}
	emu.spec = spec
	return spec, nil
}

// Removes a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) DeleteEmulatorSpec(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.emulators[specId.Value]
	if !ok {
		return nil, grpc.Errorf(codes.NotFound, "Emulator spec %q doesn't exist.", specId.Value)
	}
	delete(s.emulators, specId.Value)
	return EmptyPb, nil
}

// Lists all specs.
func (s *server) ListEmulatorSpecs(ctx context.Context, _ *pb.Empty) (*emulators.ListEmulatorSpecsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var l []*emulators.EmulatorSpec
	for _, emu := range s.emulators {
		l = append(l, emu.spec)
	}
	return &emulators.ListEmulatorSpecsResponse{Specs: l}, nil
}

func outputLogPrefixer(prefix string, in io.Reader) {
	log.Printf("Output connected for %q", prefix)
	buffReader := bufio.NewReader(in)
	for {
		line, _, err := buffReader.ReadLine()
		if err != nil {
			log.Printf("End of stream for %v, (%s).", prefix, err)
			return
		}
		log.Printf("%s: %s", prefix, line)
	}
}

// TODO: Check the context deadline, and maybe abort the start operation if its
//       exceeded. Decide whether this deadline supercedes the shared,
//       configured deadline.
func (s *server) StartEmulator(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	id := specId.Value
	log.Printf("StartEmulator %v.", id)
	s.mu.Lock()
	defer s.mu.Unlock()

	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.FailedPrecondition, "Emulator %q doesn't exist.", id)
	}

	err := emu.start()
	if err != nil {
		emu.kill()
		return nil, grpc.Errorf(codes.Unknown, "Emulator %q could not be started: %v", id, err)
	}

	// We avoid holding the lock while waiting for the emulator to start serving.
	// We don't touch the emulator instance when not holding the lock.
	s.mu.Unlock()
	started := make(chan bool, 1)
	go func() {
		_, err2 := s.waitForResolvedTarget(id, s.startDeadline)
		started <- (err2 == nil)
	}()
	ok := <-started

	s.mu.Lock()
	if !ok {
		emu.kill()
		return nil, grpc.Errorf(codes.DeadlineExceeded, "Timed-out waiting for emulator %q to start serving", id)
	}

	emu.markOnline()
	log.Printf("Emulator %q started and serving", id)
	return EmptyPb, nil
}

func (s *server) StopEmulator(ctx context.Context, specId *emulators.SpecId) (*pb.Empty, error) {
	log.Printf("StopEmulator %v.", specId)
	s.mu.Lock()
	defer s.mu.Unlock()

	id := specId.Value
	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.FailedPrecondition, "Emulator %q doesn't exist.", id)
	}
	if err := emu.kill(); err != nil {
		return nil, err
	}
	return EmptyPb, nil
}

func (s *server) ListEmulators(ctx context.Context, _ *pb.Empty) (*emulators.ListEmulatorsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nil, nil
}

// Resolves a target according to relevant specs. If no spec apply, the input
// target is returned in the response.
func (s *server) Resolve(ctx context.Context, req *emulators.ResolveRequest) (*emulators.ResolveResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Printf("Resolve %q", req.Target)
	target := []byte(req.Target)
	for _, emu := range s.emulators {
		for _, regexp := range emu.spec.TargetPattern {
			matched, err := re.Match(regexp, target)
			if err != nil {
				return nil, err
			}
			if matched {
				// TODO: What if ResolvedTarget is empty?
				log.Printf("Matched to %q", emu.spec.ResolvedTarget)
				res := &emulators.ResolveResponse{
					Target: emu.spec.ResolvedTarget,
				}
				return res, nil
			}
		}
	}
	return &emulators.ResolveResponse{Target: req.Target}, nil
}

// Waits for the given spec to have a non-empty resolved target.
// TODO: Use a condition variable instead of polling
func (s *server) waitForResolvedTarget(spec_id string, timeout time.Duration) (*emulators.EmulatorSpec, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		spec, err := s.GetEmulatorSpec(nil, &emulators.SpecId{spec_id})
		if err == nil && spec.ResolvedTarget != "" {
			return spec, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed-out waiting for resolved target: %s", spec_id)
}

type brokerGrpcServer struct {
	s          *server
	grpcServer *grpc.Server
	shutdown   chan bool
}

// The broker serving via gRPC.on the specified port.
func NewBrokerGrpcServer(port int, config *emulators.BrokerConfig, opts ...grpc.ServerOption) (*brokerGrpcServer, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Printf("failed to listen: %v", err)
		return nil, err
	}
	err = os.Setenv(BrokerAddressEnv, fmt.Sprintf("localhost:%d", port))
	if err != nil {
		log.Printf("failed to set %s: %v", BrokerAddressEnv, err)
		return nil, err
	}
	b := brokerGrpcServer{s: New(), grpcServer: grpc.NewServer(opts...), shutdown: make(chan bool, 1)}
	if config != nil {
		b.s.startDeadline = time.Duration(config.EmulatorStartDeadline.Seconds) * time.Second
	}
	emulators.RegisterBrokerServer(b.grpcServer, b.s)
	go b.grpcServer.Serve(lis)
	return &b, nil
}

// Waits for the broker to shutdown.
func (b *brokerGrpcServer) Wait() {
	<-b.shutdown
}

// Shuts down the server and frees its resources.
func (b *brokerGrpcServer) Shutdown() {
	os.Unsetenv(BrokerAddressEnv)
	b.grpcServer.Stop()
	b.s.Clear()
	b.shutdown <- true
	log.Printf("shutdown complete")
}
