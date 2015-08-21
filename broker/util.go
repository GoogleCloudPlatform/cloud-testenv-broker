/*
Copyright 2015 Google Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package broker

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"syscall"
	"time"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	emulators "google/emulators"
)

const (
	// The name of the environment variable with the broker's address.
	BrokerAddressEnv = "TESTENV_BROKER_ADDRESS"
)

func RunProcessTree(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Run()
}

// Use a session to group the child and its subprocesses, if any, for the
// purpose of terminating them as a group.
func StartProcessTree(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func KillProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	gid := -cmd.Process.Pid
	return syscall.Kill(gid, syscall.SIGINT)
}

type PortPicker interface {
	// Returns the next free port.
	Next() (int, error)
}

// Picks ports from a list of non-overlapping PortRange values.
type PortRangePicker struct {
	ranges []emulators.PortRange
	rIndex int
	last   int
}

func (p *PortRangePicker) Next() (int, error) {
	if p.last == -1 || p.last+1 >= int(p.ranges[p.rIndex].End) {
		if p.rIndex >= len(p.ranges)-1 {
			return -1, fmt.Errorf("Exhausted all ranges")
		}
		p.rIndex++
		p.last = int(p.ranges[p.rIndex].Begin)
	} else {
		p.last++
	}
	return p.last, nil
}

// Implements sort.Interface for []emulators.PortRange based on Begin.
type byBegin []emulators.PortRange

func (a byBegin) Len() int           { return len(a) }
func (a byBegin) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byBegin) Less(i, j int) bool { return a[i].Begin < a[j].Begin }

func NewPortRangePicker(ranges []emulators.PortRange) (*PortRangePicker, error) {
	// Sort the ranges, so we can easily detect overlaps.
	sort.Sort(byBegin(ranges))
	for i, r := range ranges {
		if r.Begin <= 0 {
			return nil, fmt.Errorf("Invalid PortRange: %s", r)
		}
		if r.End <= r.Begin {
			return nil, fmt.Errorf("Invalid PortRange: %s", r)
		}
		if i > 0 {
			prev := ranges[i-1]
			if r.Begin < prev.End {
				return nil, fmt.Errorf("Overlapping PortRange: %s, %s", r, prev)
			}
		}
	}
	return &PortRangePicker{ranges: ranges, rIndex: -1, last: -1}, nil
}

type BrokerClientConnection struct {
	emulators.BrokerClient
	conn *grpc.ClientConn
}

func NewBrokerClientConnection(timeout time.Duration) (*BrokerClientConnection, error) {

	brokerAddress := os.Getenv(BrokerAddressEnv)
	if brokerAddress == "" {
		return nil, fmt.Errorf("%s not specified", BrokerAddressEnv)
	}
	conn, err := grpc.Dial(brokerAddress, grpc.WithTimeout(timeout))
	if err != nil {
		log.Printf("failed to dial broker: %v", err)
		return nil, err
	}
	client := emulators.NewBrokerClient(conn)

	return &BrokerClientConnection{client, conn}, nil
}

func (bcc *BrokerClientConnection) RegisterWithBroker(ruleId string, address string, additionalTargetPatterns []string, timeout time.Duration) error {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	resp, err := bcc.BrokerClient.ListEmulators(ctx, EmptyPb)
	if err != nil {
		log.Printf("failed to list emulators: %v", err)
		return err
	}
	for _, emu := range resp.Emulators {
		if emu.Rule.RuleId != ruleId {
			continue
		}
		_, err = bcc.BrokerClient.ReportEmulatorOnline(ctx,
			&emulators.ReportEmulatorOnlineRequest{EmulatorId: emu.EmulatorId, TargetPatterns: additionalTargetPatterns, ResolvedTarget: address})
		if err != nil {
			log.Printf("failed to register emulator %q with broker: %v", emu.EmulatorId, err)
			return err
		}
		log.Printf("registered emulator %q with broker", emu.EmulatorId)
		return nil
	}
	_, err = bcc.BrokerClient.CreateResolveRule(ctx, &emulators.CreateResolveRuleRequest{
		Rule: &emulators.ResolveRule{RuleId: ruleId, TargetPatterns: additionalTargetPatterns, ResolvedTarget: address}})
	if err != nil {
		log.Printf("failed to register rule %q with broker: %v", ruleId, err)
		return err
	}
	log.Printf("registered rule %q with broker", ruleId)
	return nil
}

func (bcc *BrokerClientConnection) CreateOrUpdateRegistrationRule(ruleId string, targetPatterns []string, address string, timeout time.Duration) error {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	_, err := bcc.BrokerClient.CreateResolveRule(ctx, &emulators.CreateResolveRuleRequest{
		Rule: &emulators.ResolveRule{RuleId: ruleId, TargetPatterns: targetPatterns, ResolvedTarget: address}})
	if err != nil {
		log.Printf("failed to register rule %q with broker: %v", ruleId, err)
		return err
	}
	log.Printf("registered rule %q with broker", ruleId)
	return nil
}

func (bcc *BrokerClientConnection) Close() error {
	return bcc.conn.Close()
}

// Shortcut
func RegisterWithBroker(ruleId string, address string, additionalTargetPatterns []string, timeout time.Duration) error {
	bcc, err := NewBrokerClientConnection(timeout)
	if err != nil {
		return err
	}
	defer bcc.Close()
	return bcc.RegisterWithBroker(ruleId, address, additionalTargetPatterns, timeout)

}

// Returns the combined contents of a and b, with no duplicates.
func merge(a []string, b []string) []string {
	values := make(map[string]bool)
	for _, v := range a {
		values[v] = true
	}
	for _, v := range b {
		values[v] = true
	}
	var results []string
	for v, _ := range values {
		results = append(results, v)
	}
	return results
}
