package broker

import (
	emulators "google/emulators"
	"testing"
	"time"
)

func TestEndToEndRegisterEmulator(t *testing.T) {
	b, err := NewBrokerGrpcServer(10000)
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
	updatedSpec, err := b.WaitForResolvedTarget(spec.Id, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got := updatedSpec.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
