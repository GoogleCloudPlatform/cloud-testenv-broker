/*
Copyright 2014 Google Inc. All Rights Reserved.

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
	"google.golang.org/grpc"
	"log"
	"net/http"
	"os"
	"os/signal"

	broker "cloud-testenv-broker/broker"
	runtime "github.com/gengo/grpc-gateway/runtime"
	jsonpb "github.com/golang/protobuf/jsonpb"
	proto "github.com/golang/protobuf/proto"
	context "golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	emulators "google/emulators"
	pb "google/protobuf"
)

var (
	tls      = flag.Bool("tls", false, "Connection uses TLS if true, else plain TCP")
	certFile = flag.String("cert_file", "server1.pem", "The TLS cert file")
	keyFile  = flag.String("key_file", "server1.key", "The TLS key file")
	port     = flag.Int("port", 0,
		fmt.Sprintf("The server port. If specified, overrides the value of the %s environment variable.",
			broker.BrokerAddressEnv))
	restPort   = flag.Int("rest_port", 0, "The REST port")
	configFile = flag.String("config_file", "", "The json config file of the Cloud Broker.")
)

func init() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
	log.SetPrefix("Broker: ")
}

func runRestProxy() error {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := runtime.NewServeMux()
	conn, err := grpc.Dial(fmt.Sprintf(":%d", *port), grpc.WithInsecure())
	if err != nil {
		return err
	}
	defer conn.Close()
	err = emulators.RegisterBrokerHandler(ctx, mux, conn)
	if err != nil {
		return err
	}

	log.Printf("REST proxy listening on :%d", *restPort)
	http.ListenAndServe(fmt.Sprintf(":%d", *restPort), mux)
	return nil
}

func main() {
	log.Printf("Emulator broker starting up...")
	flag.Parse()
	if *restPort != 0 && *restPort == *port {
		log.Fatalf("--rest_port must be different from --port")
	}

	config := emulators.BrokerConfig{DefaultEmulatorStartDeadline: &pb.Duration{Seconds: 10}}
	if *configFile != "" {
		// Parse configFile and use it.
		f, err := os.Open(*configFile)
		if err != nil {
			log.Fatalf("Failed to open config file: %v", err)
		}
		err = jsonpb.Unmarshal(f, &config)
		if err != nil {
			log.Fatalf("Failed to parse config file: %v", err)
		}
	}
	log.Printf("Using configuration:\n%s", proto.MarshalTextString(&config))

	var opts []grpc.ServerOption
	if *tls {
		creds, err := credentials.NewServerTLSFromFile(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("Failed to generate credentials %v.", err)
		}
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	b, err := broker.NewBrokerGrpcServer(*port, &config, opts...)
	if err != nil {
		log.Fatalf("Failed to create broker: %v", err)
	}
	err = b.Start()
	if err != nil {
		log.Fatalf("Failed to start broker: %v", err)
	}
	die := make(chan os.Signal, 1)
	signal.Notify(die, os.Interrupt, os.Kill)
	go func() {
		<-die
		b.Shutdown()
		os.Exit(1)
	}()
	defer b.Shutdown()
	log.Printf("Broker listening on :%d.", *port)

	if *restPort != 0 {
		go func() {
			err = runRestProxy()
			if err != nil {
				log.Fatalf("Failed to run REST proxy: %v", err)
			}
		}()
	}

	b.Wait()
}
