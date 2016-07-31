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

// Package broker implements the cloud broker.
package broker

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	glog "github.com/golang/glog"
	runtime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	emulators "google/emulators"
)

type grpcServer struct {
	config     emulators.BrokerConfig
	host       string
	port       int
	s          *server
	mux        *listenerMux
	grpcServer *grpc.Server
	started    bool
	mu         sync.Mutex
	waitGroup  sync.WaitGroup
}

// NewGrpcServer returns a Broker service gRPC and HTTP/Json server listening on the specified port.
func NewGrpcServer(host string, port int, brokerDir string, config *emulators.BrokerConfig, opts ...grpc.ServerOption) (*grpcServer, error) {
	b := grpcServer{host: host, port: port, s: New(), grpcServer: grpc.NewServer(opts...), started: false}
	b.s.expander.brokerDir = brokerDir

	var err error
	if config != nil {
		b.config = *config
		if len(config.PortRanges) > 0 {
			b.s.expander.portPicker, err = NewPortRangePicker(config.PortRanges)
			if err != nil {
				return nil, err
			}
		}
		for _, e := range config.Emulators {
			_, err = b.s.CreateEmulator(nil, e)
			if err != nil {
				return nil, err
			}
		}
		for _, r := range config.Rules {
			_, err = b.s.CreateResolveRule(nil, r)
			if err != nil {
				return nil, err
			}
		}
		if config.DefaultEmulatorStartDeadline != nil {
			b.s.defaultStartDeadline = time.Duration(config.DefaultEmulatorStartDeadline.Seconds) * time.Second
		}
	}
	emulators.RegisterBrokerServer(b.grpcServer, b.s)
	return &b, nil
}

// Start starts the Broker server.
func (b *grpcServer) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", b.host, b.port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	if b.port == 0 {
		// Determine the port that was bound.
		addr = lis.Addr().String()
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("failed to determine bound port for address %v: %v", addr, err)
		}
		b.port, err = strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("unexpected port value for address %v: %v", addr, err)
		}
	}
	b.mux = newListenerMux(lis)

	err = os.Setenv(BrokerAddressEnv, addr)
	if err != nil {
		return fmt.Errorf("failed to set %s: %v", BrokerAddressEnv, err)
	}

	b.waitGroup.Add(2)
	go func() {
		b.grpcServer.Serve(b.mux.HTTP2Listener)
		b.waitGroup.Done()
	}()
	go func() {
		err := b.runRestProxy(b.mux.HTTPListener, addr)
		if err != nil {
			glog.Fatalf("failed to run REST proxy: %v", err)
		}
		b.waitGroup.Done()
	}()
	b.started = true
	return nil
}

func (b *grpcServer) Port() int {
	return b.port
}

func (s *grpcServer) shutdownHandler(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
	w.Write([]byte("Shutting down...\n"))
	go func() {
		// Allow the response message to be delivered before shutting down.
		time.Sleep(500 * time.Millisecond)
		s.Shutdown()
	}()
}

func (b *grpcServer) runRestProxy(l net.Listener, addr string) error {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// We register the HTTP handlers to mux (implements http.Handler), which
	// delegates to calls on a gRPC connection.
	mux := runtime.NewServeMux()
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	err = emulators.RegisterBrokerHandler(ctx, mux, conn)
	if err != nil {
		return err
	}

	// Add the shutdown handler.
	pat, err := runtime.NewPattern(1, []int{2, 0}, []string{"shutdown"}, "")
	if err != nil {
		return err
	}
	mux.Handle("POST", pat, b.shutdownHandler)

	http.Serve(l, &prettyJsonHandler{delegate: mux, indent: "  "})
	return nil
}

// Wait waits for the Broker server to shutdown.
func (b *grpcServer) Wait() {
	b.waitGroup.Wait()
}

// Shutdown shuts down the Broker server and frees its resources.
func (b *grpcServer) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()

	os.Unsetenv(BrokerAddressEnv)
	b.grpcServer.Stop()
	b.mux.Close()
	b.s.Clear()
	b.waitGroup.Wait()
	b.started = false
}

// Creates and starts a broker server. Simplifies testing.
func startNewBroker(config *emulators.BrokerConfig) (*grpcServer, error) {
	b, err := NewGrpcServer("localhost", 0, "brokerDir", config)
	if err != nil {
		return nil, err
	}
	err = b.Start()
	if err != nil {
		return nil, err
	}
	return b, nil
}
