// The key/value plugin of go-plugin's "negotiated" example, as a go-in-go
// module: it implements protocol versions 2 and 3 of the KV interface, and
// the host and the plugin agree on one when the host loads it.
//
// go-plugin negotiates at process start: the host sends the versions it
// supports in an environment variable, and the plugin answers with the one
// it chose in its handshake line. Here the host calls Negotiate, and the
// module applies the same rule: the highest version both sides support, or
// the oldest one the plugin has if the host doesn't say.
package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ReadFile returns the contents of a file in the host's data directory.
//
//gig:import
func ReadFile(name string) ([]byte, error)

// WriteFile replaces the contents of a file in the host's data directory.
//
//gig:import
func WriteFile(name string, data []byte) error

// kv is one version of the KV protocol.
type kv interface {
	Put(key string, value []byte) error
	Get(key string) ([]byte, error)
}

// versions are the protocol versions this plugin implements.
var versions = map[int]kv{
	2: kvV2{},
	3: kvV3{},
}

var current kv

// Negotiate picks the protocol version for this instance from the
// comma-separated versions the host supports, and returns it.
//
//gig:export
func Negotiate(hostVersions string) (int, error) {
	chosen := 0
	if hostVersions == "" {
		// A host that doesn't negotiate gets the oldest version.
		for v := range versions {
			if chosen == 0 || v < chosen {
				chosen = v
			}
		}
	}
	for _, s := range strings.Split(hostVersions, ",") {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		if _, ok := versions[v]; ok && v > chosen {
			chosen = v
		}
	}
	if chosen == 0 {
		return 0, fmt.Errorf("no common protocol version: host supports %q, plugin supports 2,3", hostVersions)
	}
	current = versions[chosen]
	return chosen, nil
}

var errNotNegotiated = errors.New("call Negotiate first")

// Put stores value under key, with the negotiated protocol version.
//
//gig:export
func Put(key string, value []byte) error {
	if current == nil {
		return errNotNegotiated
	}
	return current.Put(key, value)
}

// Get returns the value stored under key, with the negotiated protocol
// version.
//
//gig:export
func Get(key string) ([]byte, error) {
	if current == nil {
		return nil, errNotNegotiated
	}
	return current.Get(key)
}

type kvV2 struct{}

func (kvV2) Put(key string, value []byte) error {
	return WriteFile("kv_"+key, fmt.Appendf(nil, "%s\n\nWritten from plugin version 2\n", value))
}

func (kvV2) Get(key string) ([]byte, error) {
	d, err := ReadFile("kv_" + key)
	if err != nil {
		return nil, err
	}
	return append(d, "Read by plugin version 2\n"...), nil
}

type kvV3 struct{}

func (kvV3) Put(key string, value []byte) error {
	return WriteFile("kv_"+key, fmt.Appendf(nil, "%s\n\nWritten from plugin version 3\n", value))
}

func (kvV3) Get(key string) ([]byte, error) {
	d, err := ReadFile("kv_" + key)
	if err != nil {
		return nil, err
	}
	return append(d, "Read by plugin version 3\n"...), nil
}
