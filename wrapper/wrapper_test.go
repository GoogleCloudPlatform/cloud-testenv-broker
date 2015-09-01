package main

import (
	"cloud-testenv-broker/broker"
	"fmt"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	emulators "google/emulators"
	"log"
	"net/http"
	"testing"
	"time"
)

var (
	brokerHost         = "localhost"
	brokerPort         = 10000
	wrapperStartupTime = 5 * time.Second
)

func connectToBroker() (emulators.BrokerClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", brokerHost, brokerPort), grpc.WithTimeout(1*time.Second))
	if err != nil {
		log.Printf("failed to dial broker: %v", err)
		return nil, nil, err
	}

	client := emulators.NewBrokerClient(conn)
	return client, conn, nil
}

func getWithRetries(url string, timeout time.Duration) (*http.Response, error) {
	deadline := time.Now().Add(timeout)
	var err error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			return resp, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, err
}

// Runs the wrapper specifying --wrapper_check_regexp.
func TestEndToEndRegisterEmulatorWithWrapperCheckingRegex(t *testing.T) {
	b, err := broker.NewBrokerGrpcServer(brokerPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = b.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	brokerClient, conn, err := connectToBroker()
	ctx, _ := context.WithTimeout(context.Background(), 2*wrapperStartupTime)
	defer conn.Close()

	id := "end2end-wrapper"
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: id},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../wrapper/main.go",
				"--wrapper_check_url=http://localhost:12345/status",
				"--wrapper_check_regexp=ok",
				"--wrapper_resolved_target=localhost:12345",
				"--wrapper_rule_id=" + id,
				"go", "run", "../samples/emulator/main.go", "--port=12345", "--wait"},
		},
	}

	_, err = brokerClient.CreateEmulator(ctx, emu)
	if err != nil {
		t.Error(err)
	}

	// StartEmulator blocks for a while.
	started := make(chan bool, 1)
	go func() {
		_, err = brokerClient.StartEmulator(ctx, &emulators.EmulatorId{id})
		if err != nil {
			t.Fatal(err)
		}
		started <- true
	}()

	// The emulator does not immediately indicate it is serving. This first wait
	// should fail.
	select {
	case <-started:
		t.Fatalf("emulator should not be serving yet (--wait)")
	case <-time.After(wrapperStartupTime):
		break
	}

	// Tell the emulator to indicate it is serving. The new wait should succeed.
	_, err = getWithRetries("http://localhost:12345/setStatusOk", 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		break
	case <-time.After(1 * time.Second):
		t.Fatalf("emulator should be serving by now!")
	}

	rule, err := brokerClient.GetResolveRule(ctx, &emulators.ResolveRuleId{RuleId: id})
	if err != nil {
		t.Error(err)
	}

	got := rule.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// Runs the wrapper WITHOUT --wrapper_check_regexp.
// (The emulator is run with --text_status=false to support this.)
func TestEndToEndRegisterEmulatorWithWrapperCheckingResponseOnURL(t *testing.T) {
	b, err := broker.NewBrokerGrpcServer(10000, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = b.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	brokerClient, conn, err := connectToBroker()
	ctx, _ := context.WithTimeout(context.Background(), 2*wrapperStartupTime)
	defer conn.Close()

	id := "end2end-wrapper"
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: id},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../wrapper/main.go",
				"--wrapper_check_url=http://localhost:12345/status",
				"--wrapper_resolved_target=localhost:12345",
				"--wrapper_rule_id=" + id,
				"go", "run", "../samples/emulator/main.go", "--port=12345", "--text_status=false", "--wait"},
		},
	}

	_, err = brokerClient.CreateEmulator(ctx, emu)
	if err != nil {
		t.Error(err)
	}

	// StartEmulator blocks for a while.
	started := make(chan bool, 1)
	go func() {
		_, err = brokerClient.StartEmulator(ctx, &emulators.EmulatorId{EmulatorId: id})
		if err != nil {
			t.Fatal(err)
		}
		started <- true
	}()

	// The emulator does not immediately indicate it is serving. This first wait
	// should fail.
	select {
	case <-started:
		t.Fatalf("emulator should not be serving yet (--wait)")
	case <-time.After(wrapperStartupTime):
		break
	}

	// Tell the emulator to indicate it is serving. The new wait should succeed.
	_, err = getWithRetries("http://localhost:12345/setStatusOk", 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		break
	case <-time.After(1 * time.Second):
		t.Fatalf("emulator should be serving by now!")
	}

	rule, err := brokerClient.GetResolveRule(ctx, &emulators.ResolveRuleId{RuleId: id})
	if err != nil {
		t.Error(err)
	}

	got := rule.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// Runs the wrapper WITHOUT --wrapper_check_url, --wrapper_check_regexp, and
// --wrapper_resolved_target.
// (The emulator is run with --status_path=/ and --text_status=false to support
// this.)
func TestEndToEndRegisterEmulatorWithWrapperCheckingResponse(t *testing.T) {
	b, err := broker.NewBrokerGrpcServer(10000, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = b.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	brokerClient, conn, err := connectToBroker()
	ctx, _ := context.WithTimeout(context.Background(), 2*wrapperStartupTime)
	defer conn.Close()

	id := "end2end-wrapper"
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: id},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../wrapper/main.go",
				"--wrapper_rule_id=" + id,
				"go", "run", "../samples/emulator/main.go", "--port=12345", "--status_path=/", "--text_status=false", "--wait"},
		},
	}
	_, err = brokerClient.CreateEmulator(ctx, emu)
	if err != nil {
		t.Error(err)
	}

	// StartEmulator blocks for a while.
	started := make(chan bool, 1)
	go func() {
		_, err = brokerClient.StartEmulator(ctx, &emulators.EmulatorId{EmulatorId: id})
		if err != nil {
			t.Fatal(err)
		}
		started <- true
	}()

	// The emulator does not immediately indicate it is serving. This first wait
	// should fail.
	select {
	case <-started:
		t.Fatalf("emulator should not be serving yet (--wait)")
	case <-time.After(wrapperStartupTime):
		break
	}

	// Tell the emulator to indicate it is serving. The new wait should succeed.
	_, err = getWithRetries("http://localhost:12345/setStatusOk", 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		break
	case <-time.After(1 * time.Second):
		t.Fatalf("emulator should be serving by now!")
	}

	rule, err := brokerClient.GetResolveRule(ctx, &emulators.ResolveRuleId{RuleId: id})
	if err != nil {
		t.Error(err)
	}

	got := rule.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
