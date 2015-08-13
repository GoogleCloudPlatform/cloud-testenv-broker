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

package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"time"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	emulators "google/emulators"
)

var (
	// The "wrapper_" prefix should help us avoid flag name collisions.
	checkUrl       = flag.String("wrapper_check_url", "", "The URL to check for the serving state of the wrapped emulator")
	checkRegexp    = flag.String("wrapper_check_regexp", "", "If non-empty, the regular expression used to match content read from --check_url that indicates the emulator is serving")
	resolvedTarget = flag.String("wrapper_resolved_target", "", "The address the emulator can be resolved on")
	specIdFlag     = flag.String("wrapper_spec_id", "", "The id the wrapped emulator is registered as.")
)

func checkServing() bool {
	resp, err := http.Get(*checkUrl)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			if *checkRegexp == "" {
				log.Printf("check URL responded with 200")
				return true
			}
			content, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return false
			}
			matched, _ := regexp.MatchString(*checkRegexp, string(content))
			if matched {
				log.Printf("check URL has matching text: %s", content)
				return true
			}
		}
	}
	return false
}

// TODO(hbcha): Factor this out to a library, to share with the sample emulator.
func registerWithBroker(specId string, address string) (string, error) {
	brokerAddress := os.Getenv("TESTENV_BROKER_ADDRESS")
	if brokerAddress == "" {
		return "", errors.New("TESTENV_BROKER_ADDRESS not specified")
	}
	conn, err := grpc.Dial(brokerAddress, grpc.WithTimeout(1*time.Second))
	if err != nil {
		return brokerAddress, errors.New(fmt.Sprintf("failed to dial broker: %v", err))
	}
	defer conn.Close()

	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	spec := emulators.EmulatorSpec{Id: specId, ResolvedTarget: address}
	broker := emulators.NewBrokerClient(conn)
	_, err = broker.UpdateEmulatorSpec(ctx, &spec)
	if err != nil {
		return brokerAddress, err
	}
	return brokerAddress, nil
}

func main() {
	flag.Parse()
	if *checkUrl == "" {
		log.Fatalf("--wrapper_check_url not specified")
	}
	if *resolvedTarget == "" {
		log.Fatalf("--wrapper_resolved_target not specified")
	}
	if *specIdFlag == "" {
		log.Fatalf("--wrapper_spec_id not specified")
	}
	if len(flag.Args()) == 0 {
		log.Fatalf("emulator command not specified")
	}

	log.Printf("running emulator command: %s", flag.Args())
	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// TODO(hbchai): Unfortunately, for go programs, this still leaves a child
	//               process running. Figure out how to terminate the entire tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	err := cmd.Start()
	if err != nil {
		log.Fatalf("failed to start emulator command: %v", err)
	}

	log.Printf("waiting for emulator serving state check to succeed...")
	for !checkServing() {
		time.Sleep(200 * time.Millisecond)
	}

	brokerAddress, err := registerWithBroker(*specIdFlag, *resolvedTarget)
	if err != nil {
		log.Fatalf("failed to register with broker: %v", err)
	}
	log.Printf("registered with broker at %s", brokerAddress)
}
