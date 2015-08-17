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
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	broker "cloud-testenv-broker/broker"
)

var (
	register   = flag.Bool("register", false, "Whether this emulator registers with the broker by updating a ResolveRule.")
	port       = flag.Int("port", 0, "The emulator server port")
	specId     = flag.String("rule_id", "samples.emulator", "The ResolveRule id this emulator updates. Ignored when --register=false.")
	statusPath = flag.String("status_path", "/status", "The URL path where this emulator reports its status. Must begin with '/'.")
	textStatus = flag.Bool("text_status", true,
		"Whether status is indicated by text values 'ok' and 'bad'. "+
			"If false, status is indicated by response code.")
	wait = flag.Bool("wait", false, "Whether to wait for a request to '/setStatusOk' before serving")
)

type statusServer struct {
	ok     bool
	mux    *http.ServeMux
	mu     sync.Mutex
	okCond *sync.Cond
}

func newStatusServer() *statusServer {
	s := statusServer{ok: !*wait, mux: http.NewServeMux()}
	s.okCond = sync.NewCond(&s.mu)
	s.mux.HandleFunc(*statusPath, func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.ok {
			w.Write([]byte("ok\n"))
		} else {
			if *textStatus {
				w.Write([]byte("bad\n"))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
		s.mu.Unlock()
	})
	s.mux.HandleFunc("/setStatusOk", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.ok = true
		s.okCond.Broadcast()
		s.mu.Unlock()
	})
	return &s
}

func (s *statusServer) waitUntilOk() {
	s.okCond.L.Lock()
	for !s.ok {
		s.okCond.Wait()
	}
	s.okCond.L.Unlock()
}

// Starts an HTTP server whose status is read via statusPath.
func main() {
	flag.Parse()
	if *port == 0 {
		log.Fatalf("--port not specified")
	}
	if !strings.HasPrefix(*statusPath, "/") {
		log.Fatalf("--status_path must begin with '/'")
	}

	// Start serving on port.
	myAddress := fmt.Sprintf("localhost:%d", *port)
	statusServer := newStatusServer()
	s := &http.Server{
		Addr:    myAddress,
		Handler: statusServer.mux,
	}
	log.Printf("serving on port %d", *port)
	done := make(chan bool, 1)
	go func() {
		s.ListenAndServe()
		done <- true
	}()
	statusServer.waitUntilOk()
	log.Printf("serving status is ok")

	if *register {
		// Register with the broker.
		err := broker.RegisterWithBroker(*specId, myAddress, []string{}, 1*time.Second)
		if err != nil {
			log.Fatal(err)
		}
	}

	<-done
}
