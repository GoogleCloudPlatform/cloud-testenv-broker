package broker

import (
	"reflect"
	"testing"

	proto "github.com/golang/protobuf/proto"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	emulators "google/emulators"
)

var dummyRule *emulators.ResolveRule = &emulators.ResolveRule{
	RuleId:         "foo_rule",
	TargetPatterns: []string{"pattern1", "pattern2"},
}

var dummyEmulator *emulators.Emulator = &emulators.Emulator{
	EmulatorId: "foo",
	Rule:       dummyRule,
	StartCommand: &emulators.CommandLine{
		Path: "/exepath",
		Args: []string{"arg1", "arg2"},
	},
}

func TestCreateEmulator(t *testing.T) {
	s := New()
	_, err := s.CreateEmulator(nil, &emulators.CreateEmulatorRequest{Emulator: dummyEmulator})
	if err != nil {
		t.Error(err)
	}

	got, err := s.GetEmulator(nil, &emulators.EmulatorId{EmulatorId: dummyEmulator.EmulatorId})
	if err != nil {
		t.Error(err)
	}
	if !proto.Equal(got, dummyEmulator) {
		t.Errorf("Failed to find the same emulator; want = %v, got %v", dummyEmulator, got)
	}
}

func TestCreateEmulatorFailsWhenAlreadyExists(t *testing.T) {
	s := New()
	_, err := s.CreateEmulator(nil, &emulators.CreateEmulatorRequest{Emulator: dummyEmulator})
	if err != nil {
		t.Error(err)
	}

	_, err = s.CreateEmulator(nil, &emulators.CreateEmulatorRequest{Emulator: dummyEmulator})
	if err == nil {
		t.Errorf("This creation should have failed.")
	}
	if grpc.Code(err) != codes.AlreadyExists {
		t.Errorf("This creation should have failed with AlreadyExists.")
	}
}

func TestGetEmulatorWhenNotFound(t *testing.T) {
	s := New()
	_, err := s.GetEmulator(nil, &emulators.EmulatorId{"whatever"})

	if err == nil {
		t.Errorf("Get of a non existent emulator should have failed.")
	}
	if grpc.Code(err) != codes.NotFound {
		t.Errorf("Get should return NotFound as error")
	}
}

func TestListEmulators(t *testing.T) {
	s := New()
	want1 := &emulators.Emulator{EmulatorId: "foo",
		Rule:         &emulators.ResolveRule{RuleId: "foo_rule"},
		StartCommand: &emulators.CommandLine{Path: "/foo", Args: []string{"arg1", "arg2"}}}
	_, err := s.CreateEmulator(nil, &emulators.CreateEmulatorRequest{Emulator: want1})
	if err != nil {
		t.Error(err)
	}

	want2 := &emulators.Emulator{EmulatorId: "bar",
		Rule:         &emulators.ResolveRule{RuleId: "bar_rule"},
		StartCommand: &emulators.CommandLine{Path: "/bar", Args: []string{"arg1", "arg2"}}}
	_, err = s.CreateEmulator(nil, &emulators.CreateEmulatorRequest{Emulator: want2})
	if err != nil {
		t.Error(err)
	}

	want := make(map[string]*emulators.Emulator)
	want[want1.EmulatorId] = want1
	want[want2.EmulatorId] = want2

	resp, err := s.ListEmulators(nil, EmptyPb)
	if err != nil {
		t.Error(err)
	}
	got := make(map[string]*emulators.Emulator)
	for _, emu := range resp.Emulators {
		got[emu.EmulatorId] = emu
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReportEmulatorOnline(t *testing.T) {
	s := New()
	_, err := s.CreateEmulator(nil, &emulators.CreateEmulatorRequest{Emulator: dummyEmulator})
	if err != nil {
		t.Error(err)
	}

	s.emulators[dummyEmulator.EmulatorId].markStartingForTest()

	req := emulators.ReportEmulatorOnlineRequest{
		EmulatorId:     dummyEmulator.EmulatorId,
		TargetPatterns: []string{"newPattern"},
		ResolvedTarget: "somethingElse"}
	_, err = s.ReportEmulatorOnline(nil, &req)
	if err != nil {
		t.Errorf("Reporting emulator online should not have failed. %v", err)
	}

	rule, err := s.GetResolveRule(nil, &emulators.ResolveRuleId{RuleId: dummyEmulator.Rule.RuleId})
	if err != nil {
		t.Error(err)
	}
	got := rule.ResolvedTarget
	want := req.ResolvedTarget
	if got != want {
		t.Error("Want %q but got %q", want, got)
	}
	if len(rule.TargetPatterns) != len(dummyEmulator.Rule.TargetPatterns)+len(req.TargetPatterns) {
		t.Error("Target patterns were not merged correctly: %v", rule.TargetPatterns)
	}
}

func TestReportEmulatorOnlineWhenOffline(t *testing.T) {
	s := New()
	_, err := s.CreateEmulator(nil, &emulators.CreateEmulatorRequest{Emulator: dummyEmulator})
	if err != nil {
		t.Error(err)
	}

	req := emulators.ReportEmulatorOnlineRequest{
		EmulatorId:     dummyEmulator.EmulatorId,
		ResolvedTarget: "somethingElse"}
	_, err = s.ReportEmulatorOnline(nil, &req)
	if err == nil {
		t.Errorf("Reporting emulator online should have failed. %v", err)
	}
	if grpc.Code(err) != codes.FailedPrecondition {
		t.Errorf("This creation should have failed with FailedPrecondition.")
	}
}

func TestCreateResolveRule(t *testing.T) {
	s := New()
	_, err := s.CreateResolveRule(nil, &emulators.CreateResolveRuleRequest{Rule: dummyRule})
	if err != nil {
		t.Error(err)
	}

	got, err := s.GetResolveRule(nil, &emulators.ResolveRuleId{RuleId: dummyRule.RuleId})
	if err != nil {
		t.Error(err)
	}
	if !proto.Equal(got, dummyRule) {
		t.Errorf("Failed to find the same rule; want = %v, got %v", dummyRule, got)
	}
}

func TestCreateResolveRuleWhenAlreadyExists(t *testing.T) {
	s := New()
	_, err := s.CreateResolveRule(nil, &emulators.CreateResolveRuleRequest{Rule: dummyRule})
	if err != nil {
		t.Error(err)
	}

	_, err = s.CreateResolveRule(nil, &emulators.CreateResolveRuleRequest{Rule: dummyRule})
	if err == nil {
		t.Errorf("This creation should have failed.")
	}
	if grpc.Code(err) != codes.AlreadyExists {
		t.Errorf("This creation should have failed with AlreadyExists.")
	}
}

// TODO: Add a test for Resolve(). In particular the emulator starting case.
