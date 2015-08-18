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
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: "end2end_rule"},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../samples/emulator/main.go", "--register", "--port=12345", "--rule_id=" + id},
		},
	}
	_, err = b.s.CreateEmulator(ctx, &emulators.CreateEmulatorRequest{Emulator: emu})
	if err != nil {
		t.Error(err)
	}
	_, err = b.s.StartEmulator(ctx, &emulators.EmulatorId{EmulatorId: id})
	if err != nil {
		t.Error(err)
	}

	brokerClient, conn, err := connectToBroker()
	defer conn.Close()
	rule, err := brokerClient.GetResolveRule(ctx, &emulators.ResolveRuleId{RuleId: emu.Rule.RuleId})
	if err != nil {
		t.Fatal(err)
	}
	got := rule.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
