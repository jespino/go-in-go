// A key/value store plugin, as in go-plugin's "grpc" example: values are kept
// in files named kv_<key>.
//
// A module has no operating system, so it can't open files itself: it asks
// the host through the ReadFile and WriteFile imports, and the host decides
// what it may touch.
package main

import "fmt"

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
	value = fmt.Appendf(nil, "%s\n\nWritten from plugin-plain", value)
	return WriteFile("kv_"+key, value)
}

// Get returns the value stored under key.
//
//gig:export
func Get(key string) ([]byte, error) {
	return ReadFile("kv_" + key)
}
