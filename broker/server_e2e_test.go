package broker

import (
	"testing"
	"time"

	context "golang.org/x/net/context"
	emulators "google/emulators"
)

// TODO: Merge the broker comms utility code with wrapper_test.go
var (
	brokerPort          = 10000
	emulatorStartupTime = 5 * time.Second
)

func TestEndToEndRegisterEmulator(t *testing.T) {
	b, err := startNewBroker(brokerPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	id := "end2end"
	ruleId := id + "_rule"
	ctx, _ := context.WithTimeout(context.Background(), emulatorStartupTime)
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: ruleId},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../samples/emulator/main.go", "--register", "--port=12345", "--rule_id=" + ruleId},
		},
	}
	_, err = b.s.CreateEmulator(ctx, emu)
	if err != nil {
		t.Error(err)
	}
	_, err = b.s.StartEmulator(ctx, &emulators.EmulatorId{EmulatorId: id})
	if err != nil {
		t.Error(err)
	}

	conn, err := NewBrokerClientConnection(1 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	rule, err := conn.BrokerClient.GetResolveRule(ctx, &emulators.ResolveRuleId{RuleId: emu.Rule.RuleId})
	if err != nil {
		t.Fatal(err)
	}
	got := rule.ResolvedTarget
	want := "localhost:12345"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestEndToEndEmulatorCanBeRestarted(t *testing.T) {
	b, err := startNewBroker(brokerPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	id := "end2end"
	ruleId := id + "_rule"
	ctx, _ := context.WithTimeout(context.Background(), emulatorStartupTime)
	emu := &emulators.Emulator{
		EmulatorId: id,
		Rule:       &emulators.ResolveRule{RuleId: "end2end_rule"},
		StartCommand: &emulators.CommandLine{
			Path: "go",
			Args: []string{"run", "../samples/emulator/main.go", "--register", "--port=12345", "--rule_id=" + ruleId},
		},
	}
	_, err = b.s.CreateEmulator(ctx, emu)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.s.StartEmulator(ctx, &emulators.EmulatorId{EmulatorId: id})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.s.StopEmulator(ctx, &emulators.EmulatorId{EmulatorId: id})
	if err != nil {
		t.Fatal(err)
	}

	conn, err := NewBrokerClientConnection(1 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	rule, err := conn.BrokerClient.GetResolveRule(ctx, &emulators.ResolveRuleId{RuleId: emu.Rule.RuleId})
	if err != nil {
		t.Fatal(err)
	}
	got := rule.ResolvedTarget
	want := ""
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}

	ctx, _ = context.WithTimeout(context.Background(), emulatorStartupTime)
	_, err = b.s.StartEmulator(ctx, &emulators.EmulatorId{EmulatorId: id})
	if err != nil {
		t.Fatal(err)
	}

	rule, err = conn.BrokerClient.GetResolveRule(ctx, &emulators.ResolveRuleId{RuleId: emu.Rule.RuleId})
	if err != nil {
		t.Fatal(err)
	}
	got = rule.ResolvedTarget
	want = "localhost:12345"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
