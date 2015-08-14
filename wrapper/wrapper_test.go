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
	brokerHost = "localhost"
	brokerPort = 10000
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

// Runs the wrapper specifying --wrapper_check_regexp.
func TestEndToEndRegisterEmulatorWithWrapperCheckingRegex(t *testing.T) {
	b, err := broker.NewBrokerGrpcServer(brokerPort)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	brokerClient, conn, err := connectToBroker()
	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	defer conn.Close()

	id := "end2end-wrapper"
	spec := &emulators.EmulatorSpec{
		Id:            id,
		TargetPattern: []string{""},
		CommandLine: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../wrapper/main.go",
				"--wrapper_check_url=http://localhost:12345/status",
				"--wrapper_check_regexp=ok",
				"--wrapper_resolved_target=localhost:12345",
				"--wrapper_spec_id=" + id,
				"go", "run", "../samples/emulator/main.go", "--port=12345", "--wait"},
		},
	}

	_, err = brokerClient.CreateEmulatorSpec(ctx, &emulators.CreateEmulatorSpecRequest{SpecId: id, Spec: spec})
	if err != nil {
		t.Error(err)
	}

	_, err = brokerClient.StartEmulator(ctx, &emulators.SpecId{id})
	if err != nil {
		t.Error(err)
	}

	// The emulator does not immediately indicate it is serving. This first wait
	// should fail.
	updatedSpec, err := b.WaitForResolvedTarget(spec.Id, 3*time.Second)
	if err == nil {
		t.Fatalf("emulator should not be serving yet (--wait)")
	}

	// Tell the emulator to indicate it is serving. The new wait should succeed.
	_, err = http.Get("http://localhost:12345/setStatusOk")
	if err != nil {
		log.Fatal(err)
	}
	updatedSpec, err = b.WaitForResolvedTarget(spec.Id, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got := updatedSpec.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
