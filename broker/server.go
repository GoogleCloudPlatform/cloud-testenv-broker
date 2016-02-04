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
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	re "regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	runtime "github.com/gengo/grpc-gateway/runtime"
	glog "github.com/golang/glog"
	proto "github.com/golang/protobuf/proto"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	emulators "google/emulators"
	pb "google/protobuf"
)

var (
	EmptyPb = &pb.Empty{}

	portMatcher = re.MustCompile("{port:([\\w\\.-]+)}")
	envMatcher  = re.MustCompile("{env:(\\w+)}")
)

type commandExpander struct {
	brokerDir  string
	ports      map[string]int
	portPicker PortPicker
}

func newCommandExpander(brokerDir string, portPicker PortPicker) *commandExpander {
	expander := &commandExpander{brokerDir: brokerDir, portPicker: portPicker}
	expander.ports = make(map[string]int)
	return expander
}

// Expands special port and environment variable tokens in s, in-place. ports
// is the existing port substitution map, which may be modified by this
// function. pickPort is used to pick new ports.
func (expander *commandExpander) expandSpecialTokens(s *string) error {
	// Get all port names.
	m := portMatcher.FindAllStringSubmatch(*s, -1)
	if m != nil {
		for _, submatches := range m {
			for _, portName := range submatches[1:] {
				_, exists := (expander.ports)[portName]
				if !exists {
					port, err := expander.portPicker.Next()
					if err != nil {
						return fmt.Errorf("Failed to expand port token: %v", err)
					}
					expander.ports[portName] = port
					glog.Infof("Selected port for %q: %d", portName, port)
				}
			}
		}
		for portName, port := range expander.ports {
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
	// Broker directory.
	*s = strings.Replace(*s, "{dir:broker}", expander.brokerDir, -1)
	return nil
}

// Expands special port and environment variable tokens in the path and args
// of the specified command. pickPort is used to pick new ports.
func (expander *commandExpander) expand(command *emulators.CommandLine) error {
	err := expander.expandSpecialTokens(&command.Path)
	if err != nil {
		return err
	}
	for i, _ := range command.Args {
		err = expander.expandSpecialTokens(&command.Args[i])
		if err != nil {
			return err
		}
	}
	return nil
}

type localEmulator struct {
	emulator *emulators.Emulator
	cmd      *exec.Cmd
	expander *commandExpander
}

func (emu *localEmulator) start() error {
	if emu.emulator.State != emulators.Emulator_OFFLINE {
		return fmt.Errorf("Emulator %q cannot be started because it is in state %q.", emu.emulator, emu.emulator.State)
	}

	startCommand := emu.emulator.StartCommand
	err := emu.expander.expand(startCommand)
	if err != nil {
		return err
	}
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

	glog.Infof("Starting %q", emu.emulator.EmulatorId)

	err = StartProcessTree(emu.cmd)
	if err != nil {
		glog.Warningf("Error starting %q", emu.emulator.EmulatorId)
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
		glog.V(1).Infof("Emulator %q cannot be killed because it is not running", emu.emulator.EmulatorId)
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
	emulators            map[string]*localEmulator
	resolveRules         map[string]*emulators.ResolveRule
	expander             *commandExpander
	defaultStartDeadline time.Duration
	mu                   sync.Mutex
}

func New() *server {
	glog.Infof("Server created.")
	s := server{expander: newCommandExpander("", &FreePortPicker{}), defaultStartDeadline: time.Minute}
	s.Clear()
	return &s
}

// Cleans up this instance, namely its emulators map, killing any that are running.
func (s *server) Clear() {
	s.mu.Lock()
	for _, emu := range s.emulators {
		emu.kill()
	}
	s.emulators = make(map[string]*localEmulator)
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
func (s *server) CreateEmulator(ctx context.Context, req *emulators.Emulator) (*pb.Empty, error) {
	glog.V(1).Infof("CreateEmulator %v.", req)
	id := req.EmulatorId
	if req.EmulatorId == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "emulator.emulator_id was not specified")
	}
	if req.StartCommand == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "emulator.start_command was not specified")
	}
	if req.StartCommand.Path == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "emulator.start_command.path was not specified")
	}
	if req.Rule == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "Emulator %q: rule was not specified", id)
	}
	ruleId := req.Rule.RuleId
	if ruleId == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "Emulator %q: rule.rule_id was not specified", id)
	}
	err := s.checkTargetPatterns(req.Rule.TargetPatterns)
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

	emu := localEmulator{emulator: proto.Clone(req).(*emulators.Emulator), expander: s.expander}
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
	glog.V(1).Infof("Output connected for %q", prefix)
	buffReader := bufio.NewReader(in)
	for {
		line, _, err := buffReader.ReadLine()
		if err != nil {
			glog.V(1).Infof("End of stream for %v, (%s).", prefix, err)
			return
		}
		glog.Infof("%s: %s", prefix, line)
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
	glog.V(1).Infof("StartEmulator %v.", id)
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
		_, err2 := s.waitForResolvedHost(ruleId, s.startDeadline(ctx))
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

	glog.V(1).Infof("Emulator %q started and serving", id)
	return EmptyPb, nil
}

func (s *server) ReportEmulatorOnline(ctx context.Context, req *emulators.ReportEmulatorOnlineRequest) (*pb.Empty, error) {
	id := req.EmulatorId
	glog.V(1).Infof("ReportEmulatorOnline %v.", id)
	err := s.checkTargetPatterns(req.TargetPatterns)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "target_patterns invalid: %v", err)
	}
	if req.ResolvedHost == "" {
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
	rule.ResolvedHost = req.ResolvedHost
	return EmptyPb, nil
}

func (s *server) StopEmulator(ctx context.Context, req *emulators.EmulatorId) (*pb.Empty, error) {
	id := req.EmulatorId
	glog.V(1).Infof("StopEmulator %v.", id)
	s.mu.Lock()
	defer s.mu.Unlock()

	emu, exists := s.emulators[id]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Emulator %q doesn't exist.", id)
	}
	// Retract the ResolvedHost.
	emu.Emulator().Rule.ResolvedHost = ""
	if err := emu.kill(); err != nil {
		return nil, err
	}
	return EmptyPb, nil
}

func (s *server) CreateResolveRule(ctx context.Context, req *emulators.ResolveRule) (*pb.Empty, error) {
	glog.V(1).Infof("Create ResolveRule %q", req)
	if req.RuleId == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "rule.rule_id was not specified")
	}
	id := req.RuleId
	err := s.checkTargetPatterns(req.TargetPatterns)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "Resolve rule %q: target_patterns invalid: %v", id, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rule, exists := s.resolveRules[id]
	if exists {
		if proto.Equal(req, rule) {
			// The rule already exists, and it is identical to the requested rule.
			// We return success as a special case.
			return EmptyPb, nil
		}
		return nil, grpc.Errorf(codes.AlreadyExists, "Resolve rule %q already exists exist.", id)
	}
	s.resolveRules[id] = proto.Clone(req).(*emulators.ResolveRule)
	return EmptyPb, nil
}

func (s *server) GetResolveRule(ctx context.Context, req *emulators.ResolveRuleId) (*emulators.ResolveRule, error) {
	glog.V(1).Infof("Get ResolveRule %q", req)
	s.mu.Lock()
	defer s.mu.Unlock()
	rule, exists := s.resolveRules[req.RuleId]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Resolve rule %q doesn't exist.", req.RuleId)
	}
	return rule, nil
}

func (s *server) UpdateResolveRule(ctx context.Context, req *emulators.ResolveRule) (*emulators.ResolveRule, error) {
	glog.V(1).Infof("Update ResolveRule %q", req)
	s.mu.Lock()
	defer s.mu.Unlock()
	rule, exists := s.resolveRules[req.RuleId]
	if !exists {
		return nil, grpc.Errorf(codes.NotFound, "Resolve rule %q doesn't exist.", req.RuleId)
	}
	rule.TargetPatterns = merge(rule.TargetPatterns, req.TargetPatterns)
	rule.ResolvedHost = req.ResolvedHost
	return rule, nil
}

func (s *server) ListResolveRules(ctx context.Context, req *pb.Empty) (*emulators.ListResolveRulesResponse, error) {
	glog.V(1).Infof("List ResolveRules %q", req)
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := &emulators.ListResolveRulesResponse{}
	for _, rule := range s.resolveRules {
		resp.Rules = append(resp.Rules, rule)
	}
	return resp, nil
}

func computeResolveResponse(target string, rule *emulators.ResolveRule) (*emulators.ResolveResponse, error) {
	url, err := url.Parse(target)
	if err == nil && url.Scheme != "" {
		url.Host = rule.ResolvedHost
		if rule.RequiresSecureConnection {
			url.Scheme = "https"
		} else {
			url.Scheme = "http"
		}
		return &emulators.ResolveResponse{Target: url.String(), RequiresSecureConnection: rule.RequiresSecureConnection}, nil
	}
	return &emulators.ResolveResponse{Target: rule.ResolvedHost, RequiresSecureConnection: rule.RequiresSecureConnection}, nil
}

// Resolves a target according to relevant specs. If no spec apply, the input
// target is returned in the response.
func (s *server) Resolve(ctx context.Context, req *emulators.ResolveRequest) (*emulators.ResolveResponse, error) {
	glog.V(1).Infof("Resolve %q", req.Target)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the matching rule.
	var rule *emulators.ResolveRule = nil
	for _, r := range s.resolveRules {
		for _, regexp := range r.TargetPatterns {
			matched, err := re.MatchString(regexp, req.Target)
			if err != nil {
				// This is unexpected, since we should have rejected bad expressions
				// when the rule was being created. We log and move on.
				glog.Warningf("Encountered invalid target pattern: %s", regexp)
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
	if rule.ResolvedHost != "" {
		glog.V(1).Infof("Matched to %q", rule.ResolvedHost)
		return computeResolveResponse(req.Target, rule)
	}

	// The rule does not specify a resolved host. If it is associated with an
	// emulator, starting the emulator may result in a resolved host.
	emu := s.findEmulator(rule.RuleId)
	if emu == nil {
		return nil, grpc.Errorf(codes.Unavailable, "Rule %q has no resolved host (no emulator)", rule.RuleId)
	}
	if !emu.StartOnDemand {
		return nil, grpc.Errorf(codes.Unavailable,
			"Rule %q has no resolved host (emulator not running and not started on demand)", rule.RuleId)
	}

	s.mu.Unlock()
	_, err := s.StartEmulator(ctx, &emulators.EmulatorId{EmulatorId: emu.EmulatorId})
	s.mu.Lock()

	if err != nil {
		return nil, grpc.Errorf(codes.Unavailable, "Rule %q has no resolved host (emulator failed to start): %v", rule.RuleId, err)
	}
	if rule.ResolvedHost == "" {
		return nil, grpc.Errorf(codes.Unavailable, "Rule %q has no resolved host (retry?)", rule.RuleId)
	}
	glog.V(1).Infof("Matched to %q", rule.ResolvedHost)
	return computeResolveResponse(req.Target, rule)
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

// Waits for the given emulator to enter the STARTING state.
func (s *server) waitForStarting(emulatorId string, deadline time.Time) error {
	for time.Now().Before(deadline) {
		s.mu.Lock()
		emu, exists := s.emulators[emulatorId]
		if exists && emu.State() == emulators.Emulator_STARTING {
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed-out waiting for STARTING: %s", emulatorId)
}

// Waits for the given spec to have a non-empty resolved host.
func (s *server) waitForResolvedHost(ruleId string, deadline time.Time) (*emulators.ResolveRule, error) {
	for time.Now().Before(deadline) {
		rule, err := s.GetResolveRule(nil, &emulators.ResolveRuleId{RuleId: ruleId})
		if err == nil && rule.ResolvedHost != "" {
			return rule, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed-out waiting for resolved host: %s", ruleId)
}

type brokerGrpcServer struct {
	config     emulators.BrokerConfig
	host       string
	port       int
	s          *server
	mux        *listenerMux
	grpcServer *grpc.Server
	started    bool
	mu         sync.Mutex
	waitGroup  sync.WaitGroup
}

// The broker serving via gRPC.on the specified port.
func NewBrokerGrpcServer(host string, port int, brokerDir string, config *emulators.BrokerConfig, opts ...grpc.ServerOption) (*brokerGrpcServer, error) {
	b := brokerGrpcServer{host: host, port: port, s: New(), grpcServer: grpc.NewServer(opts...), started: false}
	if port > 0 {
		b.port = port
	}

	var err error
	if config != nil {
		b.config = *config
		if len(config.PortRanges) > 0 {
			b.s.expander.portPicker, err = NewPortRangePicker(config.PortRanges)
			if err != nil {
				return nil, err
			}
		}
		for _, e := range config.Emulators {
			_, err = b.s.CreateEmulator(nil, e)
			if err != nil {
				return nil, err
			}
		}
		for _, r := range config.Rules {
			_, err = b.s.CreateResolveRule(nil, r)
			if err != nil {
				return nil, err
			}
		}
		if config.DefaultEmulatorStartDeadline != nil {
			b.s.defaultStartDeadline = time.Duration(config.DefaultEmulatorStartDeadline.Seconds) * time.Second
		}
	}
	emulators.RegisterBrokerServer(b.grpcServer, b.s)
	return &b, nil
}

func (b *brokerGrpcServer) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", b.host, b.port)
	err := os.Setenv(BrokerAddressEnv, addr)
	if err != nil {
		return fmt.Errorf("failed to set %s: %v", BrokerAddressEnv, err)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	b.mux = newListenerMux(lis)

	b.waitGroup.Add(2)
	go func() {
		b.grpcServer.Serve(b.mux.HTTP2Listener)
		b.waitGroup.Done()
	}()
	go func() {
		b.runRestProxy(b.mux.HTTPListener, addr)
		b.waitGroup.Done()
	}()
	b.started = true
	return nil
}

func (b *brokerGrpcServer) runRestProxy(l net.Listener, addr string) error {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// We register the HTTP handlers to mux (implements http.Handler), which
	// delegates to calls on a gRPC connection.
	mux := runtime.NewServeMux()
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	err = emulators.RegisterBrokerHandler(ctx, mux, conn)
	if err != nil {
		return err
	}
	http.Serve(l, &prettyJsonHandler{delegate: mux, indent: "  "})
	return nil
}

// Waits for the broker to shutdown.
func (b *brokerGrpcServer) Wait() {
	b.waitGroup.Wait()
}

// Shuts down the server and frees its resources.
func (b *brokerGrpcServer) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()

	os.Unsetenv(BrokerAddressEnv)
	b.grpcServer.Stop()
	b.mux.Close()
	b.s.Clear()
	b.waitGroup.Wait()
	b.started = false
	glog.Infof("shutdown complete")
}

// Creates and starts a broker server. Simplifies testing.
func startNewBroker(port int, config *emulators.BrokerConfig) (*brokerGrpcServer, error) {
	b, err := NewBrokerGrpcServer("localhost", port, "brokerDir", config)
	if err != nil {
		return nil, err
	}
	err = b.Start()
	if err != nil {
		return nil, err
	}
	return b, nil
}
