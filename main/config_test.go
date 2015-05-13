package main

import (
	"testing"
)

func TestConfigDecode(t *testing.T) {
	config, err := Decode("tests/config.json")
	if err != nil {
		t.Fatalf("Decode failed %v", err)
	}
	reg := config.Registrations[0]

	if reg.Id != "pubsub.googleapis.com" {
		t.Fatalf("Wanted Id to be pubsub.googleapis.com but got %q.", reg.Id)
	}

	if reg.BinarySpec.Path != "fakes/pubsub_fake" {
		t.Fatalf("Wanted BinarySpec.Path to be fakes/pubsub_fake but got %q", reg.BinarySpec.Path)
	}
}
