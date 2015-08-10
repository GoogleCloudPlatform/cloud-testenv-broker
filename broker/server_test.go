package broker

import (
	emulators "google/emulators"
	"testing"
)

func TestCreateSpec(t *testing.T) {

	s := &Server{}
	req := &emulators.CreateEmulatorSpecRequest{
		SpecId: "foo",
		Spec: &emulators.EmulatorSpec{
			Id:            "foo",
			TargetPattern: []string{"foo*./", "bar*./"},
			CommandLine: &emulators.CommandLine{
				Path: "/exepath",
				Args: []string{"arg1", "arg2"},
			},
		},
	}
	se, err := s.CreateEmulatorSpec(nil, req)
	if err != nil {
		panic(err)
	}

	s.GetEmulatorSpec(nil, &emulators.SpecId{se.Id})

}
