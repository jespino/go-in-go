// The counter plugin of go-plugin's "bidirectional" example, as a go-in-go
// module. The host doesn't trust the plugin to add numbers, so Put calls
// back into the host's Sum to do it.
//
// In go-plugin, the host passes an AddHelper to Put, and the plugin reaches
// it through the gRPC broker on a second connection. In go-in-go, calling
// the host is what imports are for: Sum is a plain function call that runs
// the host's Go code and returns, in the middle of Put.
package main

import "strconv"

// Sum adds two numbers. It's the host's AddHelper.
//
//gig:import
func Sum(a, b int64) (int64, error)

// ReadFile returns the contents of a file in the host's data directory.
//
//gig:import
func ReadFile(name string) ([]byte, error)

// WriteFile replaces the contents of a file in the host's data directory.
//
//gig:import
func WriteFile(name string, data []byte) error

// Put adds value to the counter stored under key.
//
//gig:export
func Put(key string, value int64) error {
	v, _ := Get(key) // a missing counter starts at 0
	r, err := Sum(v, value)
	if err != nil {
		return err
	}
	return WriteFile("kv_"+key, strconv.AppendInt(nil, r, 10))
}

// Get returns the counter stored under key.
//
//gig:export
func Get(key string) (int64, error) {
	data, err := ReadFile("kv_" + key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(data), 10, 64)
}
