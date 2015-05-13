package main

import (
	"encoding/json"
	"io/ioutil"
)

type BinarySpec struct {
	Path string `json:path`
}

type Registration struct {
	Id            string     `json:id`
	TargetPattern string     `json:targetPattern`
	Path          string     `json:path`
	BinarySpec    BinarySpec `json:binarySpec`
}

type Config struct {
	Registrations []Registration `json:registrations`
}

func Decode(filename string) (*Config, error) {
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config

	err = json.Unmarshal(bytes, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}
