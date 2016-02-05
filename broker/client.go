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
	"os"
	"time"

	glog "github.com/golang/glog"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	emulators "google/emulators"
)

type ClientConnection struct {
	emulators.BrokerClient
	conn *grpc.ClientConn
}

func NewClientConnection(timeout time.Duration) (*ClientConnection, error) {
	brokerAddress := os.Getenv(BrokerAddressEnv)
	if brokerAddress == "" {
		return nil, fmt.Errorf("%s not specified", BrokerAddressEnv)
	}
	conn, err := grpc.Dial(brokerAddress, grpc.WithInsecure(), grpc.WithTimeout(timeout))
	if err != nil {
		glog.Warningf("failed to dial broker: %v", err)
		return nil, err
	}
	client := emulators.NewBrokerClient(conn)

	return &ClientConnection{client, conn}, nil
}

func (c *ClientConnection) RegisterWithBroker(ruleId string, address string, additionalTargetPatterns []string, timeout time.Duration) error {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	resp, err := c.BrokerClient.ListEmulators(ctx, EmptyPb)
	if err != nil {
		glog.Warningf("failed to list emulators: %v", err)
		return err
	}
	for _, emu := range resp.Emulators {
		if emu.Rule.RuleId != ruleId {
			continue
		}
		_, err = c.BrokerClient.ReportEmulatorOnline(ctx,
			&emulators.ReportEmulatorOnlineRequest{
				EmulatorId:     emu.EmulatorId,
				TargetPatterns: additionalTargetPatterns,
				ResolvedHost:   address})
		if err != nil {
			glog.Warningf("failed to register emulator %q with broker: %v", emu.EmulatorId, err)
			return err
		}
		glog.Infof("registered emulator %q with broker", emu.EmulatorId)
		return nil
	}
	_, err = c.BrokerClient.CreateResolveRule(ctx,
		&emulators.ResolveRule{
			RuleId:         ruleId,
			TargetPatterns: additionalTargetPatterns,
			ResolvedHost:   address})
	if err != nil {
		glog.Warningf("failed to register rule %q with broker: %v", ruleId, err)
		return err
	}
	glog.Infof("registered rule %q with broker", ruleId)
	return nil
}

func (c *ClientConnection) Close() error {
	return c.conn.Close()
}

// Shortcut
func RegisterWithBroker(ruleId string, address string, additionalTargetPatterns []string, timeout time.Duration) error {
	c, err := NewClientConnection(timeout)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.RegisterWithBroker(ruleId, address, additionalTargetPatterns, timeout)
}
