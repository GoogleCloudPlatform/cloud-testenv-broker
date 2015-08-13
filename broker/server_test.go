package broker

import (
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	emulators "google/emulators"
	"io/ioutil"
	"log"
	"net"
	"os"
	"reflect"
	"testing"
	"time"
)

var dummySpec *emulators.EmulatorSpec = &emulators.EmulatorSpec{
	Id:            "foo",
	TargetPattern: []string{"foo*./", "bar*./"},
	CommandLine: &emulators.CommandLine{
		Path: "/exepath",
		Args: []string{"arg1", "arg2"},
	},
}

func TestCreateSpec(t *testing.T) {

	s := New()
	want := dummySpec
	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec:   want}
	spec, err := s.CreateEmulatorSpec(nil, req)

	if err != nil {
		t.Error(err)
	}

	got, err := s.GetEmulatorSpec(nil, &emulators.SpecId{spec.Id})

	if err != nil {
		t.Error(err)
	}

	if got != want {
		t.Errorf("Failed to find back the same spec want = %v, got %v", want, got)
	}
}

func TestDoubleCreateSpec(t *testing.T) {

	s := New()
	want := dummySpec
	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec:   want}
	_, err := s.CreateEmulatorSpec(nil, req)

	if err != nil {
		t.Error(err)
	}

	spec, err := s.CreateEmulatorSpec(nil, req)

	if err == nil {
		t.Errorf("This creation should have failed.")
	}

	if grpc.Code(err) != codes.AlreadyExists {
		t.Errorf("This creation should have failed with AlreadyExists.")
	}

	if spec != nil {
		t.Errorf("It should not have returned a spec %q.", spec)
	}
}

func TestMissingSpec(t *testing.T) {
	s := New()
	_, err := s.GetEmulatorSpec(nil, &emulators.SpecId{"whatever"})

	if err == nil {
		t.Errorf("Get of a non existent spec should have failed.")
	}
	if grpc.Code(err) != codes.NotFound {
		t.Errorf("Get should return NotFound as error")
	}

}

func TestUpdateMissingSpec(t *testing.T) {
	s := New()
	_, err := s.UpdateEmulatorSpec(nil, dummySpec)

	if err == nil {
		t.Errorf("Update of a non existent spec should have failed.")
	}
	if grpc.Code(err) != codes.NotFound {
		t.Errorf("Get should return NotFound as error")
	}

}

func TestDeleteMissingSpec(t *testing.T) {
	s := New()
	_, err := s.DeleteEmulatorSpec(nil, &emulators.SpecId{"whatever"})

	if err == nil {
		t.Errorf("Delete of a non existent spec should have failed.")
	}
	if grpc.Code(err) != codes.NotFound {
		t.Errorf("Get should return NotFound as error")
	}

}

func TestDeleteSpec(t *testing.T) {
	s := New()
	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec:   dummySpec}
	spec, err := s.CreateEmulatorSpec(nil, req)

	if err != nil {
		t.Error(err)
	}
	_, err = s.DeleteEmulatorSpec(nil, &emulators.SpecId{"foo"})

	if err != nil {
		t.Error(err)
	}

	_, err = s.GetEmulatorSpec(nil, &emulators.SpecId{spec.Id})
	if err == nil {
		t.Errorf("Get of a spec  after deletion should have failed.")
	}
	if grpc.Code(err) != codes.NotFound {
		t.Errorf("Get should return NotFound as error")
	}

}

func TestUpdateSpec(t *testing.T) {
	s := New()
	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec:   dummySpec}
	_, err := s.CreateEmulatorSpec(nil, req)

	if err != nil {
		t.Error(err)
	}
	modifiedSpec := *dummySpec
	want := "somethingElse"

	modifiedSpec.ResolvedTarget = want
	_, err = s.UpdateEmulatorSpec(nil, &modifiedSpec)

	if err != nil {
		t.Errorf("Update of an existent spec should not have failed. %v", err)
	}

	newSpec, err := s.GetEmulatorSpec(nil, &emulators.SpecId{dummySpec.Id})

	if err != nil {
		t.Error(err)
	}
	got := newSpec.ResolvedTarget
	if got != want {
		t.Error("Want %q but got %q", want, got)
	}
}

func TestListSpec(t *testing.T) {

	s := New()
	want1 := &emulators.EmulatorSpec{
		Id:            "foo",
		TargetPattern: []string{"foo*./", "bar*./"},
		CommandLine: &emulators.CommandLine{
			Path: "/exepath",
			Args: []string{"arg1", "arg2"},
		},
	}

	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec:   want1}
	_, err := s.CreateEmulatorSpec(nil, req)
	if err != nil {
		t.Error(err)
	}

	want2 := &emulators.EmulatorSpec{
		Id:            "bar",
		TargetPattern: []string{"baz*./", "taz*./"},
		CommandLine: &emulators.CommandLine{
			Path: "/exepathbar",
			Args: []string{"arg1", "arg2"},
		},
	}

	req = &emulators.CreateEmulatorSpecRequest{
		SpecId: "bar",
		Spec:   want2}
	_, err = s.CreateEmulatorSpec(nil, req)

	if err != nil {
		t.Error(err)
	}

	resp, err := s.ListEmulatorSpecs(nil, EMPTY)
	if err != nil {
		t.Error(err)
	}
	want := make(map[string]*emulators.EmulatorSpec)
	want[want1.Id] = want1
	want[want2.Id] = want2

	got := make(map[string]*emulators.EmulatorSpec)
	for _, spec := range resp.Specs {
		got[spec.Id] = spec
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStartEmulator(t *testing.T) {
	s := New()
	dir, err := ioutil.TempDir("", "broker-test")
	if err != nil {
		t.Error(err)
	}
	filename := dir + "testfile"

	emu := &emulators.EmulatorSpec{
		Id:            "toucher",
		TargetPattern: []string{""},
		CommandLine: &emulators.CommandLine{
			Path: "/usr/bin/touch",
			Args: []string{filename},
		},
	}

	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "toucher",
		Spec:   emu}

	_, err = s.CreateEmulatorSpec(nil, req)
	if err != nil {
		t.Error(err)
	}
	_, err = s.StartEmulator(nil, &emulators.SpecId{"toucher"})
	if err != nil {
		t.Error(err)
	}
	time.Sleep(time.Second) // FIXME: this might be flaky

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Errorf("Emulator did not start: no file %q has been created.", filename)
	}
}

type brokerGrpcServer struct {
	s          *server
	grpcServer *grpc.Server
}

// The broker service exported via gRPC.
func newBrokerGrpcServer() *brokerGrpcServer {
	lis, err := net.Listen("tcp", ":10000")
	if err != nil {
		log.Fatalf("failed to listen: %v.", err)
	}
	b := brokerGrpcServer{s: New(), grpcServer: grpc.NewServer()}
	emulators.RegisterBrokerServer(b.grpcServer, b.s)
	go b.grpcServer.Serve(lis)
	return &b
}

func (b *brokerGrpcServer) Shutdown() {
	b.grpcServer.Stop()
	b.s.Clear()
	log.Printf("Shutdown complete.")
}

func TestEndToEndRegisterEmulator(t *testing.T) {
	b := newBrokerGrpcServer()
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

	_, err := b.s.CreateEmulatorSpec(nil, req)
	if err != nil {
		t.Error(err)
	}
	_, err = b.s.StartEmulator(nil, &emulators.SpecId{id})
	if err != nil {
		t.Error(err)
	}
	var updatedSpec *emulators.EmulatorSpec
	for i := 0; i < 100; i++ {
		updatedSpec, err = b.s.GetEmulatorSpec(nil, &emulators.SpecId{spec.Id})
		if err != nil {
			t.Error(err)
		}
		if updatedSpec.ResolvedTarget != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	got := updatedSpec.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestEndToEndRegisterEmulatorWithWrapper(t *testing.T) {
	b := newBrokerGrpcServer()
	defer b.Shutdown()

	id := "end2end-wrapper"
	spec := &emulators.EmulatorSpec{
		Id:            id,
		TargetPattern: []string{""},
		CommandLine: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../samples/wrapper/main.go", "--wrapper_check_url=http://localhost:12345/status",
				"--wrapper_check_regexp=ok", "--wrapper_resolved_target=localhost:12345",
				"--wrapper_spec_id=" + id, "go", "run",
				"../samples/emulator/main.go", "--port=12345", "--spec_id=" + id},
		},
	}
	_, err := b.s.CreateEmulatorSpec(nil, &emulators.CreateEmulatorSpecRequest{SpecId: id, Spec: spec})
	if err != nil {
		t.Error(err)
	}
	_, err = b.s.StartEmulator(nil, &emulators.SpecId{id})
	if err != nil {
		t.Error(err)
	}
	var updatedSpec *emulators.EmulatorSpec
	for i := 0; i < 100; i++ {
		updatedSpec, err = b.s.GetEmulatorSpec(nil, &emulators.SpecId{spec.Id})
		if err != nil {
			t.Error(err)
			continue
		}
		if updatedSpec.ResolvedTarget != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	got := updatedSpec.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestResolve(t *testing.T) {
	want := "booya"
	s := New()
	spec := &emulators.EmulatorSpec{
		Id:             "foo",
		TargetPattern:  []string{"foo.*bar"},
		ResolvedTarget: want,
		CommandLine: &emulators.CommandLine{
			Path: "/exepath",
			Args: []string{"arg1", "arg2"},
		},
	}
	_, err := s.CreateEmulatorSpec(nil, &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec:   spec})

	if err != nil {
		t.Error(err)
	}

	// TODO: Test with start_on_demand.
	resp, err := s.Resolve(nil, &emulators.ResolveRequest{Target: "foobarbaz"})
	if err != nil {
		t.Error(err)
	}
	got := resp.Target
	if got != want {
		t.Errorf("Want %q but got %q", want, got)
	}

	want = "fobaz"
	resp, err = s.Resolve(nil, &emulators.ResolveRequest{Target: want})
	if err != nil {
		t.Error(err)
	}

	got = resp.Target
	if got != want {
		t.Errorf("Want %q but got %q", want, got)
	}
}
