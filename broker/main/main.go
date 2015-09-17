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
	"os"
	"os/signal"

	broker "cloud-testenv-broker/broker"
	jsonpb "github.com/golang/protobuf/jsonpb"
	proto "github.com/golang/protobuf/proto"
	"google.golang.org/grpc/credentials"
	emulators "google/emulators"
	pb "google/protobuf"
)

var (
	port = flag.Int("port", 0,
		fmt.Sprintf("The server port. If specified, overrides the value of the %s environment variable.",
			broker.BrokerAddressEnv))
	// TODO(hbchai): Should we accept multiple config files?
	configFile = flag.String("config_file", "", "The json config file of the Cloud Broker.")
)

func init() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
	log.SetPrefix("Broker: ")
}

func main() {
	log.Printf("Emulator broker starting up...")
	flag.Parse()

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

	b, err := broker.NewBrokerGrpcServer(*port, &config)
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
	b.Wait()
}
