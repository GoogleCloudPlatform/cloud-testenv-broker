// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build windows

package broker

import (
	"fmt"
	"os/exec"
)

func runProcessTree(cmd *exec.Cmd) error {
	return cmd.Run()
}

func startProcessTree(cmd *exec.Cmd) error {
	return cmd.Start()
}

// We use "taskkill /T" to kill the process tree.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid)).Run()
	if err != nil {
		return err
	}
	_, err = cmd.Process.Wait()
	return err
}
