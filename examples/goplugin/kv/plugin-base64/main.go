// A second implementation of the same key/value interface as plugin-plain,
// storing values base64-encoded. It exports and imports the same functions
// with the same signatures, so the host loads it with the same bindings,
// the way go-plugin's "grpc" example swaps plugin-go-grpc, plugin-go-netrpc
// and plugin-python behind one shared.KV interface.
package main

import (
	"encoding/base64"
	"fmt"
)

// ReadFile returns the contents of a file in the host's data directory.
//
//gig:import
func ReadFile(name string) ([]byte, error)

// WriteFile replaces the contents of a file in the host's data directory.
//
//gig:import
func WriteFile(name string, data []byte) error

// Put stores value under key.
//
//gig:export
func Put(key string, value []byte) error {
	value = fmt.Appendf(nil, "%s\n\nWritten from plugin-base64", value)
	return WriteFile("kv_"+key, []byte(base64.StdEncoding.EncodeToString(value)))
}

// Get returns the value stored under key.
//
//gig:export
func Get(key string) ([]byte, error) {
	data, err := ReadFile("kv_" + key)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(string(data))
}
