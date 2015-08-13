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
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
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
func registerWithBroker(specId string, address string) error {
	brokerAddress := os.Getenv("TESTENV_BROKER_ADDRESS")
	if brokerAddress == "" {
		return errors.New("TESTENV_BROKER_ADDRESS not specified")
	}
	conn, err := grpc.Dial(brokerAddress, grpc.WithTimeout(1*time.Second))
	if err != nil {
		log.Printf("failed to dial broker: %v", err)
		return err
	}
	defer conn.Close()

	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	spec := emulators.EmulatorSpec{Id: specId, ResolvedTarget: address}
	broker := emulators.NewBrokerClient(conn)
	_, err = broker.UpdateEmulatorSpec(ctx, &spec)
	if err != nil {
		log.Printf("failed to register with broker: %v", err)
		return err
	}
	log.Printf("registered with broker at %s", brokerAddress)
	return nil
}

func killEmulatorGroupAndExit(cmd *exec.Cmd, code *int) {
	log.Printf("Sending SIGINT to emulator subprocess(es)...")
	gid := -cmd.Process.Pid
	syscall.Kill(gid, syscall.SIGINT)
	os.Exit(*code)
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
	// Use a session to group the child and its subprocesses, if any, for the
	// purpose of terminating them as a group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err := cmd.Start()
	if err != nil {
		log.Fatalf("failed to start emulator command: %v", err)
	}

	// Kill the wrapped process and any of its children on both normal exit and
	// exit due to a signal. Code following this must set exitCode and return
	// instead of calling os.Exit() directly.
	exitCode := 0
	die := make(chan os.Signal, 1)
	signal.Notify(die, os.Interrupt, os.Kill)
	go func() {
		<-die
		killEmulatorGroupAndExit(cmd, &exitCode)
	}()
	defer func() { killEmulatorGroupAndExit(cmd, &exitCode) }()

	log.Printf("waiting for emulator serving state check to succeed...")
	for !checkServing() {
		time.Sleep(200 * time.Millisecond)
	}

	err = registerWithBroker(*specIdFlag, *resolvedTarget)
	if err != nil {
		exitCode = 1
		time.Sleep(time.Minute)
		return
	}

	// We run until the emulator exits.
	cmd.Wait()
}
