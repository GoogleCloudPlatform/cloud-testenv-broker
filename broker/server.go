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

	proto "github.com/golang/protobuf/proto"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	emulators "google/emulators"
	pb "google/protobuf"
)

var (
	EmptyPb = &pb.Empty{}

	config *Config
)

// TODO: We should rename this, so we don't have "emulator.emulator".
type emulator struct {
	emulator *emulators.Emulator
	cmd      *exec.Cmd
}

func (emu *emulator) start() error {
	if emu.emulator.State != emulators.Emulator_OFFLINE {
		return fmt.Errorf("Emulator %q cannot be started because it is in state %q.", emu.emulator, emu.emulator.State)
	}

	startCommand := emu.emulator.StartCommand
	cmd := exec.Command(startCommand.Path, startCommand.Args...)

	// Create stdout, stderr streams of type io.Reader
	pout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	go outputLogPrefixer(emu.emulator.EmulatorId, pout)

	perr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	go outputLogPrefixer("ERR "+emu.emulator.EmulatorId, perr)
	emu.cmd = cmd
	emu.emulator.State = emulators.Emulator_STARTING

	log.Printf("Starting %q", emu.emulator.EmulatorId)

	err = StartProcessTree(emu.cmd)
	if err != nil {
		log.Printf("Error starting %q", emu.emulator.EmulatorId)
	}
	return nil
}

func (emu *emulator) markOnline() error {
	if emu.emulator.State != emulators.Emulator_STARTING {
		return fmt.Errorf("Emulator %q cannot be marked online: %s", emu.emulator.EmulatorId, emu.emulator.State)
	}
	emu.emulator.State = emulators.Emulator_ONLINE
	return nil
}

func (emu *emulator) kill() error {
	if emu.emulator.State == emulators.Emulator_OFFLINE {
		log.Printf("Emulator %q cannot be killed because it is not running", emu.emulator.EmulatorId)
		return nil
	}
	err := KillProcessTree(emu.cmd)
	emu.emulator.State = emulators.Emulator_OFFLINE
	return err
}

type server struct {
	emulators     map[string]*emulator
	resolveRules  map[string]*emulators.ResolveRule
	startDeadline time.Duration
	mu            sync.Mutex
}

func New() *server {
	log.Printf("Server created.")
	return &server{
		emulators:     make(map[string]*emulator),
		resolveRules:  make(map[string]*emulators.ResolveRule),
		startDeadline: time.Minute}
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
func (s *server) CreateEmulator(ctx context.Context, req *emulators.CreateEmulatorRequest) (*pb.Empty, error) {
	log.Printf("CreateEmulator %v.", req.Emulator)
	id := req.Emulator.EmulatorId
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.emulators[id]
	if ok {
		return nil, grpc.Errorf(codes.AlreadyExists, "Emulator %q already exists.", id)
	}

	s.emulators[id] = &emulator{emulator: proto.Clone(req.Emulator).(*emulators.Emulator)}
	return EmptyPb, nil
}

// Finds a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) GetEmulator(ctx context.Context, emulatorId *emulators.EmulatorId) (*emulators.Emulator, error) {
	id := emulatorId.EmulatorId
	s.mu.Lock()
	defer s.mu.Unlock()
	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Emulator %q doesn't exist.", id)
	}
	return emu.emulator, nil
}

// Removes a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) DeleteEmulator(ctx context.Context, emulatorId *emulators.EmulatorId) (*pb.Empty, error) {
	id := emulatorId.EmulatorId
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Emulator %q doesn't exist.", id)
	}
	delete(s.emulators, id)
	return EmptyPb, nil
}

// Lists all specs.
func (s *server) ListEmulators(ctx context.Context, _ *pb.Empty) (*emulators.ListEmulatorsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var l []*emulators.Emulator
	for _, emu := range s.emulators {
		l = append(l, emu.emulator)
	}
	return &emulators.ListEmulatorsResponse{Emulators: l}, nil
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
func (s *server) StartEmulator(ctx context.Context, emulatorId *emulators.EmulatorId) (*pb.Empty, error) {
	id := emulatorId.EmulatorId
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

func (s *server) StopEmulator(ctx context.Context, emulatorId *emulators.EmulatorId) (*pb.Empty, error) {
	id := emulatorId.EmulatorId
	log.Printf("StopEmulator %v.", id)
	s.mu.Lock()
	defer s.mu.Unlock()

	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.FailedPrecondition, "Emulator %q doesn't exist.", id)
	}
	if err := emu.kill(); err != nil {
		return nil, err
	}
	return EmptyPb, nil
}

func (s *server) CreateResolveRule(ctx context.Context, req *emulators.CreateResolveRuleRequest) (*pb.Empty, error) {
	id := req.Rule.RuleId
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.resolveRules[id]
	if exists {
		return nil, grpc.Errorf(codes.AlreadyExists, "Resolve rule %q already exists exist.", id)
	}
	s.resolveRules[id] = proto.Clone(req.Rule).(*emulators.ResolveRule)
	return EmptyPb, nil
}

func (s *server) GetResolveRule(ctx context.Context, req *emulators.ResolveRuleId) (*emulators.ResolveRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rule, exists := s.resolveRules[req.RuleId]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Resolve rule %q doesn't exist.", req.RuleId)
	}
	return rule, nil
}

func (s *server) UpdateResolveRule(ctx context.Context, req *emulators.ResolveRule) (*pb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rule, exists := s.resolveRules[req.RuleId]
	if exists {
		if rule.ResolvedTarget != "" {
			return nil, grpc.Errorf(codes.AlreadyExists, "Resolved target already exists for rule %q: %s", rule.RuleId, rule.ResolvedTarget)
		}
		rule.TargetPatterns = merge(rule.TargetPatterns, req.TargetPatterns)
		rule.ResolvedTarget = req.ResolvedTarget
	} else {
		s.resolveRules[req.RuleId] = req
	}

	return EmptyPb, nil
}

func (s *server) DeleteResolveRule(ctx context.Context, req *emulators.ResolveRuleId) (*pb.Empty, error) {
	return EmptyPb, nil
}

func (s *server) ListResolveRules(ctx context.Context, req *pb.Empty) (*emulators.ListResolveRulesResponse, error) {
	return nil, nil
}

// Resolves a target according to relevant specs. If no spec apply, the input
// target is returned in the response.
func (s *server) Resolve(ctx context.Context, req *emulators.ResolveRequest) (*emulators.ResolveResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("Resolve %q", req.Target)
	target := []byte(req.Target)
	for _, rule := range s.resolveRules {
		for _, regexp := range rule.TargetPatterns {
			matched, err := re.Match(regexp, target)
			if err != nil {
				return nil, err
			}
			if matched {
				// TODO: What if ResolvedTarget is empty?
				log.Printf("Matched to %q", rule.ResolvedTarget)
				return &emulators.ResolveResponse{Target: rule.ResolvedTarget}, nil
			}
		}
	}
	return &emulators.ResolveResponse{Target: req.Target}, nil
}

// Waits for the given spec to have a non-empty resolved target.
// TODO: Use a condition variable instead of polling
func (s *server) waitForResolvedTarget(ruleId string, timeout time.Duration) (*emulators.ResolveRule, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rule, err := s.GetResolveRule(nil, &emulators.ResolveRuleId{RuleId: ruleId})
		if err == nil && rule.ResolvedTarget != "" {
			return rule, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed-out waiting for resolved target: %s", ruleId)
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
		b.s.startDeadline = time.Duration(config.DefaultEmulatorStartDeadline.Seconds) * time.Second
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
