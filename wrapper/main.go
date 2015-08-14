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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"time"

	broker "cloud-testenv-broker/broker"
)

var (
	// The "wrapper_" prefix should help us avoid flag name collisions.
	checkUrl = flag.String("wrapper_check_url", "",
		"The URL to check for the serving state of the wrapped emulator. "+
			"If unspecified, the value of --wrapper_resolved_target is used.")
	checkRegexp    = flag.String("wrapper_check_regexp", "", "If non-empty, the regular expression used to match content read from --check_url that indicates the emulator is serving")
	resolvedTarget = flag.String("wrapper_resolved_target", "",
		"The address the emulator can be resolved on. "+
			"If unspecified, and a '--port=<PORT>' argument is present in the emulator command, the value 'localhost:<PORT>' is used with that port value.")
	specIdFlag = flag.String("wrapper_spec_id", "", "The id the wrapped emulator is registered as.")
)

func findPort(args []string) int {
	portRegexp, _ := regexp.Compile("^--port=(\\d+)$")
	for _, arg := range args {
		m := portRegexp.FindStringSubmatch(arg)
		if m != nil {
			port, err := strconv.Atoi(m[1])
			if err != nil {
				log.Fatalf("failed to convert to int: %s", m[1])
			}
			return port
		}
	}
	return -1
}

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

func killEmulatorGroupAndExit(cmd *exec.Cmd, code *int) {
	log.Printf("Sending SIGINT to emulator subprocess(es)...")
	err := broker.KillProcessTree(cmd)
	if err != nil {
		log.Printf("failed to kill process tree: %v", err)
	}
	os.Exit(*code)
}

func main() {
	flag.Parse()
	if *specIdFlag == "" {
		log.Fatalf("--wrapper_spec_id not specified")
	}
	if len(flag.Args()) == 0 {
		log.Fatalf("emulator command not specified")
	}
	if *resolvedTarget == "" {
		port := findPort(flag.Args())
		if port == -1 {
			log.Fatalf("--wrapper_resolved_target not specified")
		}
		*resolvedTarget = fmt.Sprintf("localhost:%d", port)
	}
	if *checkUrl == "" {
		if strings.HasPrefix(*resolvedTarget, "http") {
			*checkUrl = *resolvedTarget
		} else {
			*checkUrl = fmt.Sprintf("http://%s", *resolvedTarget)
		}
		log.Printf("using check URL: %s", *checkUrl)
	}

	log.Printf("running emulator command: %s", flag.Args())
	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Use a session to group the child and its subprocesses, if any, for the
	// purpose of terminating them as a group.
	err := broker.StartProcessTree(cmd)
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
	defer killEmulatorGroupAndExit(cmd, &exitCode)

	log.Printf("waiting for emulator serving state check to succeed...")
	for !checkServing() {
		time.Sleep(200 * time.Millisecond)
	}

	err = broker.RegisterWithBroker(*specIdFlag, *resolvedTarget, 1*time.Second)
	if err != nil {
		exitCode = 1
		time.Sleep(time.Minute)
		return
	}

	// We run until the emulator exits.
	cmd.Wait()
}
