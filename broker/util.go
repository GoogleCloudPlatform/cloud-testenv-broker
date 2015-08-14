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
	codes "google.golang.org/grpc/codes"
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

func RegisterWithBroker(specId string, address string, additionalTargetPatterns []string, timeout time.Duration) error {
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

	// First test if we don't already exist.
	_, err = client.GetEmulatorSpec(ctx, &emulators.SpecId{specId})

	if err != nil {
		if grpc.Code(err) == codes.NotFound {
			_, err = client.CreateEmulatorSpec(ctx, &emulators.CreateEmulatorSpecRequest{
				SpecId: specId,
				Spec: &emulators.EmulatorSpec{Id: specId,
					ResolvedTarget: address,
					TargetPattern:  additionalTargetPatterns,
				},
			})
			if err != nil {
				return err
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
	spec := emulators.EmulatorSpec{Id: specId, ResolvedTarget: address}
	_, err = client.UpdateEmulatorSpec(ctx, &spec)
	if err != nil {
		log.Printf("failed to register %q with broker at %s: %v", specId, brokerAddress, err)
		return err
	}
	log.Printf("registered %q with broker at %s", specId, brokerAddress)
	return nil
}
