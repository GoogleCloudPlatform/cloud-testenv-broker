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
	gid := -cmd.Process.Pid
	return syscall.Kill(gid, syscall.SIGINT)
}

func RegisterWithBroker(ruleId string, address string, additionalTargetPatterns []string, timeout time.Duration) error {
	brokerAddress := os.Getenv(BrokerAddressEnv)
	if brokerAddress == "" {
		return fmt.Errorf("%s not specified", BrokerAddressEnv)
	}
	conn, err := grpc.Dial(brokerAddress, grpc.WithTimeout(timeout))
	if err != nil {
		log.Printf("failed to dial broker: %v", err)
		return err
	}
	defer conn.Close()

	client := emulators.NewBrokerClient(conn)
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	resp, err := client.ListEmulators(ctx, EmptyPb)
	if err != nil {
		log.Printf("failed to list emulators: %v", err)
		return err
	}
	for _, emu := range resp.Emulators {
		if emu.Rule.RuleId != ruleId {
			continue
		}
		_, err = client.ReportEmulatorOnline(ctx,
			&emulators.ReportEmulatorOnlineRequest{EmulatorId: emu.EmulatorId, TargetPatterns: additionalTargetPatterns, ResolvedTarget: address})
		if err != nil {
			log.Printf("failed to register emulator %q with broker at %s: %v", emu.EmulatorId, brokerAddress, err)
			return err
		}
		log.Printf("registered emulator %q with broker at %s", emu.EmulatorId, brokerAddress)
		return nil
	}
	_, err = client.CreateResolveRule(ctx, &emulators.CreateResolveRuleRequest{
		Rule: &emulators.ResolveRule{RuleId: ruleId, TargetPatterns: additionalTargetPatterns, ResolvedTarget: address}})
	if err != nil {
		log.Printf("failed to register rule %q with broker at %s: %v", ruleId, brokerAddress, err)
		return err
	}
	log.Printf("registered rule %q with broker at %s", ruleId, brokerAddress)
	return nil
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
