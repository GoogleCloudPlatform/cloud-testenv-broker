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
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"time"

	broker "cloud-testenv-broker/broker"
	glog "github.com/golang/glog"
)

var (
	checkUrl = flag.String("check_url", "",
		"The URL to check for the serving state of the wrapped emulator. "+
			"If unspecified, the value of --resolved_host is used.")
	checkRegexp  = flag.String("check_regexp", "", "If non-empty, the regular expression used to match content read from --check_url that indicates the emulator is serving")
	resolvedHost = flag.String("resolved_host", "",
		"The address the emulator can be resolved on. "+
			"If unspecified, and a '--port=<PORT>' argument is present in the emulator command, the value 'localhost:<PORT>' is used with that port value.")
	ruleId = flag.String("rule_id", "", "The ResolvedRule id the wrapped emulator is registered as.")
)

func findPort(args []string) int {
	portRegexp, _ := regexp.Compile("^--port=(\\d+)$")
	for _, arg := range args {
		m := portRegexp.FindStringSubmatch(arg)
		if m != nil {
			port, err := strconv.Atoi(m[1])
			if err != nil {
				glog.Fatalf("failed to convert to int: %s", m[1])
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
				glog.Infof("check URL responded with 200")
				return true
			}
			content, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return false
			}
			matched, _ := regexp.MatchString(*checkRegexp, string(content))
			if matched {
				glog.Infof("check URL has matching text: %s", content)
				return true
			}
		}
	}
	return false
}

func killEmulatorGroupAndExit(cmd *exec.Cmd, code *int) {
	glog.Infof("Killing emulator subprocess(es)...")
	err := broker.KillProcessTree(cmd)
	if err != nil {
		glog.Warningf("failed to kill process tree: %v", err)
	}
	os.Exit(*code)
}

func main() {
	flag.Set("alsologtostderr", "true")
	flag.Parse()
	if *ruleId == "" {
		glog.Fatalf("--rule_id not specified")
	}
	if len(flag.Args()) == 0 {
		glog.Fatalf("emulator command not specified")
	}
	if *resolvedHost == "" {
		port := findPort(flag.Args())
		if port == -1 {
			glog.Fatalf("--resolved_host not specified")
		}
		*resolvedHost = fmt.Sprintf("localhost:%d", port)
	}
	if *checkUrl == "" {
		if strings.HasPrefix(*resolvedHost, "http") {
			*checkUrl = *resolvedHost
		} else {
			*checkUrl = fmt.Sprintf("http://%s", *resolvedHost)
		}
		glog.Infof("using check URL: %s", *checkUrl)
	}

	glog.Infof("running emulator command: %s", flag.Args())
	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Use a session to group the child and its subprocesses, if any, for the
	// purpose of terminating them as a group.
	err := broker.StartProcessTree(cmd)
	if err != nil {
		glog.Fatalf("failed to start emulator command: %v", err)
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

	glog.Infof("waiting for emulator serving state check to succeed...")
	for !checkServing() {
		time.Sleep(200 * time.Millisecond)
	}

	err = broker.RegisterWithBroker(*ruleId, *resolvedHost, []string{}, 1*time.Second)
	if err != nil {
		exitCode = 1
		time.Sleep(time.Minute)
		return
	}

	// We run until the emulator exits.
	cmd.Wait()
}
