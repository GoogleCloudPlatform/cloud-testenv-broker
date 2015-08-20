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
	"strconv"
	"strings"
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

	portMatcher = re.MustCompile("{port:(\\w+)}")
	envMatcher  = re.MustCompile("{env:(\\w+)}")
)

// Expands special port and environment variable tokens in s, in-place. ports
// is the existing port substitution map, which may be modified by this
// function. pickPort is used to pick new ports.
func expandSpecialTokens(s *string, ports *map[string]int, pickPort func() int) {
	// Get all port names.
	m := portMatcher.FindAllStringSubmatch(*s, -1)
	if m != nil {
		for _, submatches := range m {
			for _, portName := range submatches[1:] {
				port, exists := (*ports)[portName]
				if !exists {
					port = pickPort()
					(*ports)[portName] = port
					log.Printf("Selected port for %q: %d", portName, port)
				}
			}
		}
		for portName, port := range *ports {
			*s = strings.Replace(*s, fmt.Sprintf("{port:%s}", portName), strconv.Itoa(port), -1)
		}
	}
	m = envMatcher.FindAllStringSubmatch(*s, -1)
	if m != nil {
		envs := make(map[string]string)
		for _, submatches := range m {
			for _, envName := range submatches[1:] {
				envs[envName] = os.Getenv(envName)
			}
		}
		for envName, env := range envs {
			*s = strings.Replace(*s, fmt.Sprintf("{env:%s}", envName), env, -1)
		}
	}
}

type startableEmulator interface {
	start() error
	markStartingForTest() error
	markOnline() error
	kill() error

	Emulator() *emulators.Emulator
	State() emulators.Emulator_State
}

// TODO: We should rename this, so we don't have "emulator.emulator".
type localEmulator struct {
	emulator *emulators.Emulator
	cmd      *exec.Cmd
}

func (emu *localEmulator) start() error {
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

func (emu *localEmulator) markStartingForTest() error {
	if emu.emulator.State != emulators.Emulator_OFFLINE {
		return fmt.Errorf("Emulator %q cannot be marked STARTING: %s", emu.emulator.EmulatorId, emu.emulator.State)
	}
	emu.emulator.State = emulators.Emulator_STARTING
	return nil
}

func (emu *localEmulator) markOnline() error {
	if emu.emulator.State != emulators.Emulator_STARTING {
		return fmt.Errorf("Emulator %q cannot be marked ONLINE: %s", emu.emulator.EmulatorId, emu.emulator.State)
	}
	emu.emulator.State = emulators.Emulator_ONLINE
	return nil
}

func (emu *localEmulator) kill() error {
	if emu.emulator.State == emulators.Emulator_OFFLINE {
		log.Printf("Emulator %q cannot be killed because it is not running", emu.emulator.EmulatorId)
		return nil
	}
	err := KillProcessTree(emu.cmd)
	emu.emulator.State = emulators.Emulator_OFFLINE
	return err
}

func (emu *localEmulator) Emulator() *emulators.Emulator {
	return emu.emulator
}

func (emu *localEmulator) State() emulators.Emulator_State {
	return emu.emulator.State
}

type server struct {
	emulators            map[string]startableEmulator
	resolveRules         map[string]*emulators.ResolveRule
	defaultStartDeadline time.Duration
	mu                   sync.Mutex
}

func New() *server {
	log.Printf("Server created.")
	s := server{defaultStartDeadline: time.Minute}
	s.Clear()
	return &s
}

// Cleans up this instance, namely its emulators map, killing any that are running.
func (s *server) Clear() {
	s.mu.Lock()
	for _, emu := range s.emulators {
		emu.kill()
	}
	s.emulators = make(map[string]startableEmulator)
	s.resolveRules = make(map[string]*emulators.ResolveRule)
	s.mu.Unlock()
}

// Checks whether the target pattern expressions are valid.
func (s *server) checkTargetPatterns(patterns []string) error {
	for _, p := range patterns {
		_, err := re.Compile(p)
		if err != nil {
			return fmt.Errorf("Invalid target pattern %s: %v", p, err)
		}
	}
	return nil
}

// Creates a spec to resolve targets to specified emulator endpoints.
// If a spec with this id already exists, returns ALREADY_EXISTS.
func (s *server) CreateEmulator(ctx context.Context, req *emulators.CreateEmulatorRequest) (*pb.Empty, error) {
	log.Printf("CreateEmulator %v.", req.Emulator)
	id := req.Emulator.EmulatorId
	if req.Emulator == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "emulator was not specified")
	}
	if req.Emulator.EmulatorId == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "emulator.emulator_id was not specified")
	}
	if req.Emulator.StartCommand == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "emulator.start_command was not specified")
	}
	if req.Emulator.StartCommand.Path == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "emulator.start_command.path was not specified")
	}
	if req.Emulator.Rule == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "Emulator %q: rule was not specified", id)
	}
	ruleId := req.Emulator.Rule.RuleId
	if ruleId == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "Emulator %q: rule.rule_id was not specified", id)
	}
	err := s.checkTargetPatterns(req.Emulator.Rule.TargetPatterns)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "Emulator %q: rule.target_patterns invalid: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.emulators[id]
	if exists {
		return nil, grpc.Errorf(codes.AlreadyExists, "Emulator %q already exists.", id)
	}
	_, exists = s.resolveRules[ruleId]
	if exists {
		return nil, grpc.Errorf(codes.AlreadyExists, "ResolveRule %q already exists.", ruleId)
	}

	emu := localEmulator{emulator: proto.Clone(req.Emulator).(*emulators.Emulator)}
	emu.emulator.State = emulators.Emulator_OFFLINE
	s.emulators[id] = &emu
	s.resolveRules[ruleId] = emu.emulator.Rule // shared
	return EmptyPb, nil
}

// Finds a spec, by id. Returns NOT_FOUND if the spec doesn't exist.
func (s *server) GetEmulator(ctx context.Context, req *emulators.EmulatorId) (*emulators.Emulator, error) {
	id := req.EmulatorId
	s.mu.Lock()
	defer s.mu.Unlock()
	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Emulator %q doesn't exist.", id)
	}
	return emu.Emulator(), nil
}

// Lists all specs.
func (s *server) ListEmulators(ctx context.Context, _ *pb.Empty) (*emulators.ListEmulatorsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var l []*emulators.Emulator
	for _, emu := range s.emulators {
		l = append(l, emu.Emulator())
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

func (s *server) startDeadline(ctx context.Context) time.Time {
	// A context deadline takes precedence over the default start deadline.
	deadline := time.Now().Add(s.defaultStartDeadline)
	if ctx != nil {
		d, ok := ctx.Deadline()
		if ok {
			deadline = d
		}
	}
	return deadline
}

func (s *server) StartEmulator(ctx context.Context, req *emulators.EmulatorId) (*pb.Empty, error) {
	id := req.EmulatorId
	log.Printf("StartEmulator %v.", id)
	s.mu.Lock()
	defer s.mu.Unlock()

	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Emulator %q doesn't exist.", id)
	}
	if emu.State() == emulators.Emulator_ONLINE {
		return nil, grpc.Errorf(codes.AlreadyExists, "Emulator %q is already running.", id)
	}
	killOnFailure := false
	if emu.State() == emulators.Emulator_OFFLINE {
		// A single execution context should transition the emulator to STARTING.
		// Other contexts should wait for the start to complete.
		err := emu.start()
		if err != nil {
			emu.kill()
			return nil, grpc.Errorf(codes.Unknown, "Emulator %q could not be started: %v", id, err)
		}
		killOnFailure = true
	}

	ruleId := emu.Emulator().Rule.RuleId

	// We avoid holding the lock while waiting for the emulator to start serving.
	// We don't touch the emulator instance when not holding the lock.
	s.mu.Unlock()
	started := make(chan bool, 1)
	go func() {
		_, err2 := s.waitForResolvedTarget(ruleId, s.startDeadline(ctx))
		started <- (err2 == nil)
	}()
	ok := <-started

	s.mu.Lock()
	if !ok {
		if killOnFailure {
			// Only the execution context that started the emulator should kill it.
			emu.kill()
		}
		return nil, grpc.Errorf(codes.DeadlineExceeded, "Timed-out waiting for emulator %q to start serving", id)
	}

	log.Printf("Emulator %q started and serving", id)
	return EmptyPb, nil
}

func (s *server) ReportEmulatorOnline(ctx context.Context, req *emulators.ReportEmulatorOnlineRequest) (*pb.Empty, error) {
	id := req.EmulatorId
	log.Printf("ReportEmulatorOnline %v.", id)
	err := s.checkTargetPatterns(req.TargetPatterns)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "target_patterns invalid: %v", err)
	}
	if req.ResolvedTarget == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "resolved_target was not specified")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Emulator %q doesn't exist.", id)
	}
	err = emu.markOnline()
	if err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "%v", err)
	}
	rule := emu.Emulator().Rule
	rule.TargetPatterns = merge(rule.TargetPatterns, req.TargetPatterns)
	rule.ResolvedTarget = req.ResolvedTarget
	return EmptyPb, nil
}

func (s *server) StopEmulator(ctx context.Context, req *emulators.EmulatorId) (*pb.Empty, error) {
	id := req.EmulatorId
	log.Printf("StopEmulator %v.", id)
	s.mu.Lock()
	defer s.mu.Unlock()

	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Emulator %q doesn't exist.", id)
	}
	// Retract the ResolvedTarget.
	emu.Emulator().Rule.ResolvedTarget = ""
	if err := emu.kill(); err != nil {
		return nil, err
	}
	return EmptyPb, nil
}

func (s *server) CreateResolveRule(ctx context.Context, req *emulators.CreateResolveRuleRequest) (*pb.Empty, error) {
	log.Printf("Create ResolveRule %q", req)
	if req.Rule == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "rule was not specified")
	}
	if req.Rule.RuleId == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "rule.rule_id was not specified")
	}
	id := req.Rule.RuleId
	err := s.checkTargetPatterns(req.Rule.TargetPatterns)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "Resolve rule %q: target_patterns invalid: %v", id, err)
	}
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
	log.Printf("Get ResolveRule %q", req)
	s.mu.Lock()
	defer s.mu.Unlock()
	rule, exists := s.resolveRules[req.RuleId]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Resolve rule %q doesn't exist.", req.RuleId)
	}
	return rule, nil
}

func (s *server) ListResolveRules(ctx context.Context, req *pb.Empty) (*emulators.ListResolveRulesResponse, error) {
	log.Printf("List ResolveRules %q", req)
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := &emulators.ListResolveRulesResponse{}
	for _, rule := range s.resolveRules {
		resp.Rules = append(resp.Rules, rule)
	}
	return resp, nil
}

// Resolves a target according to relevant specs. If no spec apply, the input
// target is returned in the response.
func (s *server) Resolve(ctx context.Context, req *emulators.ResolveRequest) (*emulators.ResolveResponse, error) {
	log.Printf("Resolve %q", req.Target)
	s.mu.Lock()
	defer s.mu.Unlock()

	var rule *emulators.ResolveRule = nil
	for _, r := range s.resolveRules {
		for _, regexp := range r.TargetPatterns {
			matched, err := re.MatchString(regexp, req.Target)
			if err != nil {
				// This is unexpected, since we should have rejected bad expressions
				// when the rule was being created. We log and move on.
				log.Printf("Encountered invalid target pattern: %s", regexp)
				continue
			}
			if matched {
				rule = r
				break
			}
		}
	}
	if rule == nil {
		return &emulators.ResolveResponse{Target: req.Target}, nil
	}
	if rule.ResolvedTarget != "" {
		log.Printf("Matched to %q", rule.ResolvedTarget)
		return &emulators.ResolveResponse{Target: rule.ResolvedTarget}, nil
	}
	emu := s.findEmulator(rule.RuleId)
	if emu == nil {
		return nil, grpc.Errorf(codes.Unavailable, "Rule %q has no resolved target (no emulator)", rule.RuleId)
	}
	if !emu.StartOnDemand {
		return nil, grpc.Errorf(codes.Unavailable,
			"Rule %q has no resolved target (emulator not running and not started on demand)", rule.RuleId)
	}

	s.mu.Unlock()
	_, err := s.StartEmulator(ctx, &emulators.EmulatorId{EmulatorId: emu.EmulatorId})
	s.mu.Lock()

	if err != nil {
		return nil, grpc.Errorf(codes.Unavailable, "Rule %q has no resolved target (emulator failed to start)", rule.RuleId)
	}
	if rule.ResolvedTarget == "" {
		return nil, grpc.Errorf(codes.Unavailable, "Rule %q has no resolved target (retry?)", rule.RuleId)
	}
	log.Printf("Matched to %q", rule.ResolvedTarget)
	return &emulators.ResolveResponse{Target: rule.ResolvedTarget}, nil
}

// REQUIRES s.mu.Lock().
func (s *server) findEmulator(ruleId string) *emulators.Emulator {
	for _, emu := range s.emulators {
		if emu.Emulator().Rule.RuleId == ruleId {
			return emu.Emulator()
		}
	}
	return nil
}

// Waits for the given spec to have a non-empty resolved target.
func (s *server) waitForResolvedTarget(ruleId string, deadline time.Time) (*emulators.ResolveRule, error) {
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
	config       emulators.BrokerConfig
	lis          net.Listener
	s            *server
	grpcServer   *grpc.Server
	started      bool
	mu           sync.Mutex
	shutdownCond *sync.Cond
}

// The broker serving via gRPC.on the specified port.
func NewBrokerGrpcServer(port int, config *emulators.BrokerConfig, opts ...grpc.ServerOption) (*brokerGrpcServer, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %v", err)
	}
	b := brokerGrpcServer{lis: lis, s: New(), grpcServer: grpc.NewServer(opts...), started: false}
	b.shutdownCond = sync.NewCond(&b.mu)
	if config != nil {
		b.config = *config
		b.s.defaultStartDeadline = time.Duration(config.DefaultEmulatorStartDeadline.Seconds) * time.Second
	}
	emulators.RegisterBrokerServer(b.grpcServer, b.s)
	err = b.Start()
	if err != nil {
		log.Printf("failed to start broker: %v", err)
		return nil, err
	}
	return &b, nil
}

func (b *brokerGrpcServer) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started {
		return nil
	}
	err := os.Setenv(BrokerAddressEnv, b.lis.Addr().String())
	if err != nil {
		return fmt.Errorf("failed to set %s: %v", BrokerAddressEnv, err)
	}
	go b.grpcServer.Serve(b.lis)
	b.started = true
	return nil
}

// Waits for the broker to shutdown.
func (b *brokerGrpcServer) Wait() {
	b.mu.Lock()
	for b.started {
		b.shutdownCond.Wait()
	}
	b.mu.Unlock()
}

// Shuts down the server and frees its resources.
func (b *brokerGrpcServer) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()

	os.Unsetenv(BrokerAddressEnv)
	b.grpcServer.Stop()
	b.s.Clear()
	b.started = false
	b.shutdownCond.Broadcast()
	log.Printf("shutdown complete")
}
