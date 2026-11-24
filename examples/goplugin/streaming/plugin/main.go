// The file streamer plugin of go-plugin's "streaming" example, as a go-in-go
// module: the host streams data into a file through the plugin, and the
// plugin streams it back, in chunks, both ways.
//
// go-plugin streams to stay under gRPC's message size limit. go-in-go has no
// such limit (a []byte argument is copied straight into the module's heap),
// but chunks still keep memory flat for large data: neither side ever holds
// the whole file. Each direction maps to a go-in-go call pattern:
//
//   - host to plugin (gRPC client streaming): the host calls the WriteChunk
//     export once per chunk, then CloseWrite.
//   - plugin to host (gRPC server streaming): during the Read export, the
//     module calls the Send import once per chunk.
//
// The module can't open files itself, so it goes through file handles the
// host hands out (the File* imports).
package main

import (
	"errors"
	"strconv"
	"strings"
)

// Log emits a structured log line through the host's logger. attrs holds
// key/value pairs, each element separated by a NUL byte.
//
//gig:import
func Log(level int, msg string, attrs string)

// FileOpen opens a file in the host's data directory, for writing
// (created or truncated) or for reading, and returns a handle.
//
//gig:import
func FileOpen(name string, write bool) (int64, error)

// FileWrite writes b to an open file.
//
//gig:import
func FileWrite(fd int64, b []byte) error

// FileRead reads up to n bytes from an open file. It returns no bytes at
// the end of the file.
//
//gig:import
func FileRead(fd int64, n int) ([]byte, error)

// FileClose closes an open file.
//
//gig:import
func FileClose(fd int64) error

// Send passes one chunk of a Read to the host.
//
//gig:import
func Send(chunk []byte) error

func debug(msg string, kv ...string) { Log(-4, msg, strings.Join(kv, "\x00")) }

var (
	path      string
	chunkSize int
	writing   int64 = -1 // handle of the file being written, -1 if none
)

// Configure sets the file to stream to and from, and the chunk size.
//
//gig:export
func Configure(p string, size int64) error {
	if size <= 0 {
		return errors.New("chunk size must be positive")
	}
	path, chunkSize = p, int(size)
	return nil
}

// WriteChunk appends one chunk to the file, creating or truncating it on the
// first chunk of a write.
//
//gig:export
func WriteChunk(chunk []byte) error {
	if writing < 0 {
		debug("FileStreamer: Write", "path", path)
		fd, err := FileOpen(path, true)
		if err != nil {
			return err
		}
		writing = fd
	}
	return FileWrite(writing, chunk)
}

// CloseWrite ends a write started by WriteChunk.
//
//gig:export
func CloseWrite() error {
	if writing < 0 {
		return nil
	}
	err := FileClose(writing)
	writing = -1
	debug("FileStreamer: Write finished")
	return err
}

// Read streams the file to the host through Send, in chunks of the
// configured size, and returns the number of chunks.
//
//gig:export
func Read() (int, error) {
	debug("FileStreamer: Read", "path", path)
	fd, err := FileOpen(path, false)
	if err != nil {
		return 0, err
	}
	defer FileClose(fd)
	chunks := 0
	for {
		b, err := FileRead(fd, chunkSize)
		if err != nil {
			return chunks, err
		}
		if len(b) == 0 {
			debug("FileStreamer: Read finished", "chunks", strconv.Itoa(chunks))
			return chunks, nil
		}
		if err := Send(b); err != nil {
			return chunks, err
		}
		chunks++
	}
}
