package main_test

import (
	"cloud-testenv-broker/broker"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	context "golang.org/x/net/context"
	emulators "google/emulators"
)

var (
	tmpDir              string
	brokerPort          int
	emulatorPath        string
	launcherStartupTime = 5 * time.Second
)

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

// The entrypoint.
func TestMain(m *testing.M) {
	var exitCode int
	err := setUp()
	if err != nil {
		log.Printf("Setup error: %v", err)
		exitCode = 1
	} else {
		exitCode = m.Run()
	}
	tearDown()
	os.Exit(exitCode)
}

// TODO(hbchai): Share this code with server_test.go.
func setUp() error {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "server_test")
	if err != nil {
		return fmt.Errorf("Failed to create temp dir: %v", err)
	}
	log.Printf("Created temp dir: %s", tmpDir)
	path, err := buildSampleEmulator(tmpDir)
	if err != nil {
		return fmt.Errorf("Failed to build sample emulator: %v", err)
	}
	log.Printf("Successfully built Sample emulator: %s", path)
	emulatorPath = path

	portPicker := &broker.FreePortPicker{}
	brokerPort, err = portPicker.Next()
	if err != nil {
		return fmt.Errorf("Failed to pick a free port: %v", err)
	}

	return nil
}

func tearDown() {
	err := os.RemoveAll(tmpDir)
	if err == nil {
		log.Printf("Deleted temp dir: %s", tmpDir)
	} else {
		log.Printf("Failed to delete temp dir: %v", err)
	}
}

// Builds the sample emulator so that it can run directly, i.e. NOT via
// "go run". Returns the path to the resulting binary.
func buildSampleEmulator(outputDir string) (string, error) {
	output := filepath.Join(outputDir, "sample_emulator")
	cmd := exec.Command("go", "build", "-o", output, "../samples/emulator/emulator.go")
	log.Printf("Running: %s", cmd.Args)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return output, nil
}

func emulatorPort(brokerClient emulators.BrokerClient, id string) (int, error) {
	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	resp, err := brokerClient.GetEmulator(ctx, &emulators.EmulatorId{EmulatorId: id})
	if err != nil {
		return 0, err
	}
	for _, arg := range resp.StartCommand.Args {
		if strings.HasPrefix(arg, "--port=") {
			return strconv.Atoi(arg[7:])
		}
	}
	return 0, fmt.Errorf("Failed to find port argument for emulator: %s", id)
}

func TestEndToEndRegisterEmulatorWithLauncherCheckingRegex(t *testing.T) {
	// TODO(hbchai): Use startNewBroker()?
	b, err := broker.NewBrokerGrpcServer("localhost", brokerPort, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = b.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	conn, err := broker.NewBrokerClientConnection(1 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	brokerClient := conn.BrokerClient
	ctx, _ := context.WithTimeout(context.Background(), 2*launcherStartupTime)
	defer conn.Close()

	id := "end2end-launcher"
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: id},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "launcher.go",
				"--check_url=http://localhost:{port:main}/status",
				"--check_regexp=ok",
				"--resolved_host=localhost:{port:main}",
				"--rule_id=" + id,
				emulatorPath, "--port={port:main}", "--wait"},
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
	case <-time.After(launcherStartupTime):
		break
	}

	// Tell the emulator to indicate it is serving. The new wait should succeed.
	port, err := emulatorPort(brokerClient, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = getWithRetries(fmt.Sprintf("http://localhost:%d/setStatusOk", port), 1*time.Second)
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

	got := rule.ResolvedHost
	want := fmt.Sprintf("localhost:%d", port)

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// Runs the launcher WITHOUT --check_regexp.
// (The emulator is run with --text_status=false to support this.)
func TestEndToEndRegisterEmulatorWithLauncherCheckingResponseOnURL(t *testing.T) {
	b, err := broker.NewBrokerGrpcServer("localhost", brokerPort, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = b.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	conn, err := broker.NewBrokerClientConnection(1 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	brokerClient := conn.BrokerClient
	ctx, _ := context.WithTimeout(context.Background(), 2*launcherStartupTime)
	defer conn.Close()

	id := "end2end-launcher"
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: id},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "launcher.go",
				"--check_url=http://localhost:{port:main}/status",
				"--resolved_host=localhost:{port:main}",
				"--rule_id=" + id,
				emulatorPath, "--port={port:main}", "--text_status=false", "--wait"},
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
	case <-time.After(launcherStartupTime):
		break
	}

	// Tell the emulator to indicate it is serving. The new wait should succeed.
	port, err := emulatorPort(brokerClient, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = getWithRetries(fmt.Sprintf("http://localhost:%d/setStatusOk", port), 1*time.Second)
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

	got := rule.ResolvedHost
	want := fmt.Sprintf("localhost:%d", port)

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// Runs the launcher WITHOUT --check_url, --check_regexp, and --resolved_host.
// (The emulator is run with --status_path=/ and --text_status=false to support
// this.)
func TestEndToEndRegisterEmulatorWithLauncherCheckingResponse(t *testing.T) {
	b, err := broker.NewBrokerGrpcServer("localhost", brokerPort, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = b.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	conn, err := broker.NewBrokerClientConnection(1 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	brokerClient := conn.BrokerClient
	ctx, _ := context.WithTimeout(context.Background(), 2*launcherStartupTime)
	defer conn.Close()

	id := "end2end-launcher"
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: id},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "launcher.go",
				"--rule_id=" + id,
				emulatorPath, "--port=12345", "--status_path=/", "--text_status=false", "--wait"},
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
	case <-time.After(launcherStartupTime):
		break
	}

	// Tell the emulator to indicate it is serving. The new wait should succeed.
	port, err := emulatorPort(brokerClient, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = getWithRetries(fmt.Sprintf("http://localhost:%d/setStatusOk", port), 1*time.Second)
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

	got := rule.ResolvedHost
	want := fmt.Sprintf("localhost:%d", port)

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
