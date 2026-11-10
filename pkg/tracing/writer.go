// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package tracing

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer writes trace events to disk with buffering.
type Writer struct {
	mu        sync.Mutex
	file      *os.File
	buf       *bufio.Writer
	flushTick *time.Ticker
	done      chan struct{}
	first     bool // track if we've written the first event
}

// TraceMetadata contains trace session information.
type TraceMetadata struct {
	SessionID   string   `json:"session_id"`
	SessionName string   `json:"session_name"`
	StartTime   int64    `json:"start_time"`
	EndTime     *int64   `json:"end_time,omitempty"`
	Hosts       []string `json:"hosts,omitempty"`
	TestName    string   `json:"test_name,omitempty"`
}

// NewWriter creates a new trace writer.
func NewWriter(dir, sessionName string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create trace dir: %w", err)
	}

	filename := fmt.Sprintf("%s_%d.json", sessionName, time.Now().Unix())
	path := filepath.Join(dir, filename)

	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create trace file: %w", err)
	}

	w := &Writer{
		file:  file,
		buf:   bufio.NewWriterSize(file, 64*1024), // 64KB buffer
		done:  make(chan struct{}),
		first: true,
	}

	// Write opening
	w.buf.WriteString(`{"traceEvents":[`)

	// Start periodic flush
	w.flushTick = time.NewTicker(5 * time.Second)
	go w.flushLoop()

	return w, nil
}

// WriteEvent writes a trace event.
func (w *Writer) WriteEvent(event TraceEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Add comma separator after first event
	if !w.first {
		w.buf.WriteByte(',')
	}
	w.first = false

	// Write newline for readability
	w.buf.WriteByte('\n')

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	_, err = w.buf.Write(data)
	return err
}

// Flush forces a flush to disk.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Flush()
}

// Close closes the writer.
func (w *Writer) Close() error {
	close(w.done)
	w.flushTick.Stop()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Write closing
	w.buf.WriteString("\n]}")
	w.buf.Flush()

	return w.file.Close()
}

func (w *Writer) flushLoop() {
	for {
		select {
		case <-w.done:
			return
		case <-w.flushTick.C:
			w.Flush()
		}
	}
}

// Path returns the trace file path.
func (w *Writer) Path() string {
	return w.file.Name()
}
