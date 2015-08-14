package broker

import (
	"fmt"
	"log"
	"testing"
	"time"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	emulators "google/emulators"
)

// TODO: Merge the broker comms utility code with wrapper_test.go
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

func TestEndToEndRegisterEmulator(t *testing.T) {
	b, err := NewBrokerGrpcServer(10000, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	id := "end2end"
	spec := &emulators.EmulatorSpec{
		Id:            id,
		TargetPattern: []string{""},
		CommandLine: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../samples/emulator/main.go", "--register", "--port=12345", "--spec_id=" + id},
		},
	}
	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: id,
		Spec:   spec}

	_, err = b.s.CreateEmulatorSpec(nil, req)
	if err != nil {
		t.Error(err)
	}
	_, err = b.s.StartEmulator(nil, &emulators.SpecId{id})
	if err != nil {
		t.Error(err)
	}

	brokerClient, conn, err := connectToBroker()
	defer conn.Close()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	updatedSpec, err := brokerClient.GetEmulatorSpec(ctx, &emulators.SpecId{Value: id})
	if err != nil {
		t.Fatal(err)
	}
	got := updatedSpec.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
