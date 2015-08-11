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
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	emulators "google/emulators"
)

var (
	register = flag.Bool("register", false, "Whether this emulator registers with the broker")
	port     = flag.Int("port", 0, "The emulator server port")
	specId   = flag.String("spec_id", "samples.emulator", "The id this emulator registers as")
	wait     = flag.Bool("wait", false, "Whether to wait for a request to '/setStatusOk' before serving")
)

type statusServer struct {
	ok     bool
	mux    *http.ServeMux
	mu     sync.Mutex
	okCond *sync.Cond
}

func newStatusServer(ok bool) *statusServer {
	s := statusServer{ok: ok, mux: http.NewServeMux()}
	s.okCond = sync.NewCond(&s.mu)
	s.mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.ok {
			w.Write([]byte("ok\n"))
		} else {
			w.Write([]byte("bad\n"))
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

func registerWithBroker(brokerAddress string, myAddress string) error {
	conn, err := grpc.Dial(brokerAddress, grpc.WithTimeout(1*time.Second))
	if err != nil {
		return errors.New(fmt.Sprintf("failed to dial broker: %v", err))
	}
	defer conn.Close()

	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	spec := emulators.EmulatorSpec{Id: *specId, ResolvedTarget: myAddress}
	broker := emulators.NewBrokerClient(conn)
	_, err = broker.UpdateEmulatorSpec(ctx, &spec)
	if err != nil {
		return err
	}
	return nil
}

// Starts an HTTP server whose status is read via '/status'.
func main() {
	flag.Parse()
	if *port == 0 {
		log.Fatalf("--port not specified")
	}

	// Start serving on port.
	myAddress := fmt.Sprintf("localhost:%d", *port)
	statusServer := newStatusServer(!*wait)
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
		brokerAddress := os.Getenv("TESTENV_BROKER_ADDRESS")
		if brokerAddress == "" {
			log.Fatalf("TESTENV_BROKER_ADDRESS not specified")
		}
		err := registerWithBroker(brokerAddress, myAddress)
		if err != nil {
			log.Fatalf("failed to register with broker: %v", err)
		}
		log.Printf("registered with broker at %s", brokerAddress)
	}

	<-done
}
