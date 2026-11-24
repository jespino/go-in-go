// go-plugin's "streaming" example with go-in-go: the host writes data to a
// file through the plugin and reads it back, streamed in chunks both ways.
//
//	go-in-go build examples/goplugin/streaming/plugin
//	go run ./examples/goplugin/streaming myfile
//
// See plugin/main.go for how each stream direction maps to go-in-go calls.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/jespino/go-in-go/examples/goplugin/hostfs"
	"github.com/jespino/go-in-go/examples/goplugin/streaming/shared"
)

// host implements shared.Imports: logging, file handles for the plugin, and
// the receiving end of Read's stream.
type host struct {
	logger *slog.Logger
	dir    hostfs.Dir

	mu     sync.Mutex
	files  map[int64]*os.File
	nextFD int64

	received *bytes.Buffer // where Send puts the chunks of a Read
}

func (h *host) Log(level int, msg string, attrs string) {
	var args []any
	if attrs != "" {
		for _, a := range strings.Split(attrs, "\x00") {
			args = append(args, a)
		}
	}
	h.logger.Log(context.Background(), slog.Level(level), msg, args...)
}

func (h *host) FileOpen(name string, write bool) (int64, error) {
	p, err := h.dir.Path(name)
	if err != nil {
		return 0, err
	}
	flags := os.O_RDONLY
	if write {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(p, flags, 0o644)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextFD++
	h.files[h.nextFD] = f
	return h.nextFD, nil
}

func (h *host) file(fd int64) (*os.File, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, ok := h.files[fd]
	if !ok {
		return nil, fmt.Errorf("bad file handle %d", fd)
	}
	return f, nil
}

func (h *host) FileWrite(fd int64, b []byte) error {
	f, err := h.file(fd)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return err
}

func (h *host) FileRead(fd int64, n int) ([]byte, error) {
	f, err := h.file(fd)
	if err != nil {
		return nil, err
	}
	b := make([]byte, n)
	n, err = io.ReadFull(f, b)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		err = nil
	}
	return b[:n], err
}

func (h *host) FileClose(fd int64) error {
	f, err := h.file(fd)
	if err != nil {
		return err
	}
	h.mu.Lock()
	delete(h.files, fd)
	h.mu.Unlock()
	return f.Close()
}

func (h *host) Send(chunk []byte) error {
	if h.received == nil {
		return errors.New("Send outside of a Read")
	}
	h.received.Write(chunk)
	return nil
}

// streamer is the host-side client, like go-plugin's StreamerGRPCClient: it
// turns Write and Read into chunked streams.
type streamer struct {
	m         *shared.Module
	h         *host
	chunkSize int
}

func (s *streamer) Configure(path string, chunkSize int) error {
	s.chunkSize = chunkSize
	return s.m.Configure(path, int64(chunkSize))
}

// Write streams b to the plugin in chunks.
func (s *streamer) Write(b []byte) error {
	for len(b) > 0 {
		n := min(s.chunkSize, len(b))
		if err := s.m.WriteChunk(b[:n]); err != nil {
			return err
		}
		b = b[n:]
	}
	return s.m.CloseWrite()
}

// Read collects the chunks the plugin streams back.
func (s *streamer) Read() ([]byte, int, error) {
	var buf bytes.Buffer
	s.h.received = &buf
	defer func() { s.h.received = nil }()
	chunks, err := s.m.Read()
	return buf.Bytes(), chunks, err
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("expected path to file as an argument")
	}
	path := os.Args[1]
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	plugin := os.Getenv("STREAMER_PLUGIN")
	if plugin == "" {
		plugin = "examples/goplugin/streaming/plugin/streamer.gig"
	}
	h := &host{logger: logger, dir: ".", files: map[int64]*os.File{}}
	logger.Debug("loading the plugin")
	m, err := shared.Load(plugin, h, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer m.Close()

	s := &streamer{m: m, h: h}
	if err := s.Configure(path, 10); err != nil {
		log.Fatal(err)
	}
	data := []byte("Lorem ipsum dolor sit amet")
	if err := s.Write(data); err != nil {
		log.Fatal(err)
	}
	logger.Debug("writing finished")

	b, chunks, err := s.Read()
	if err != nil {
		log.Fatal(err)
	}
	logger.Debug(fmt.Sprintf("received %d bytes in %d chunks", len(b), chunks), "bytes", string(b))
	if !bytes.Equal(b, data) {
		log.Fatalf("read back %q, want %q", b, data)
	}
}
