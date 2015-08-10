package broker

import (
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	emulators "google/emulators"
	"testing"
)

func TestCreateSpec(t *testing.T) {

	s := New()
	want := &emulators.EmulatorSpec{
		Id:            "foo",
		TargetPattern: []string{"foo*./", "bar*./"},
		CommandLine: &emulators.CommandLine{
			Path: "/exepath",
			Args: []string{"arg1", "arg2"},
		},
	}

	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec:   want}
	spec, err := s.CreateEmulatorSpec(nil, req)

	if err != nil {
		panic(err)
	}

	got, err := s.GetEmulatorSpec(nil, &emulators.SpecId{spec.Id})

	if err != nil {
		panic(err)
	}

	if got != want {
		t.Errorf("Failed to find back the same spec want = %v, got %v", want, got)
	}
}

func TestDoubleCreateSpec(t *testing.T) {

	s := New()
	want := &emulators.EmulatorSpec{
		Id:            "foo",
		TargetPattern: []string{"foo*./", "bar*./"},
		CommandLine: &emulators.CommandLine{
			Path: "/exepath",
			Args: []string{"arg1", "arg2"},
		},
	}

	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec:   want}
	_, err := s.CreateEmulatorSpec(nil, req)

	if err != nil {
		panic(err)
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
