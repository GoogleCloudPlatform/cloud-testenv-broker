package broker

import (
	"fmt"
	"testing"
	"time"

	context "golang.org/x/net/context"
	emulators "google/emulators"
)

// TODO: Merge the broker comms utility code with wrapper_test.go
var (
	emulatorStartupTime = 5 * time.Second
)

func TestEndToEndRegisterEmulator(t *testing.T) {
	b, err := startNewBroker(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	_, err = b.s.CreateEmulator(nil, realEmulator)
	if err != nil {
		t.Error(err)
	}
	_, err = b.s.StartEmulator(nil, &emulators.EmulatorId{EmulatorId: realEmulator.EmulatorId})
	if err != nil {
		t.Error(err)
	}

	conn, err := NewClientConnection(1 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, _ := context.WithTimeout(context.Background(), emulatorStartupTime)
	resp, err := conn.BrokerClient.Resolve(ctx, &emulators.ResolveRequest{Target: "real_service"})
	if err != nil {
		t.Fatal(err)
	}
	got := resp.Target
	port, err := realEmulatorPort(b)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("localhost:%d", port)

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
