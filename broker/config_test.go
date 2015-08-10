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

package broker

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
