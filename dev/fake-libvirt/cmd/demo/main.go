// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package main provides a demo server for fake-libvirt with API and sample data.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/api"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/state"
)

var (
	apiPort    = flag.Int("api-port", 8081, "HTTP API port")
	socketPath = flag.String("socket", "/tmp/demo-libvirt.sock", "Unix socket path")
	webuiDir   = flag.String("webui-dir", "", "Path to web UI directory (serves on :8000 if set)")
)

func main() {
	flag.Parse()

	log.Println("Starting fake-libvirt demo...")

	// Create state with sample domains
	st := state.New()
	addSampleDomains(st)

	// Start API server
	apiServer := api.NewServer(st)
	go func() {
		addr := fmt.Sprintf(":%d", *apiPort)
		log.Printf("API server listening on %s", addr)
		if err := apiServer.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			log.Printf("API server error: %v", err)
		}
	}()

	// Start libvirt socket server
	os.Remove(*socketPath)
	listener, err := net.Listen("unix", *socketPath)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *socketPath, err)
	}
	defer os.Remove(*socketPath)
	log.Printf("Libvirt socket listening on %s", *socketPath)

	go acceptConnections(listener, st)

	// Optionally serve web UI
	if *webuiDir != "" {
		go func() {
			log.Printf("Web UI serving %s on :8000", *webuiDir)
			fs := http.FileServer(http.Dir(*webuiDir))
			if err := http.ListenAndServe(":8000", fs); err != nil {
				log.Printf("Web UI server error: %v", err)
			}
		}()
	}

	log.Println("")
	log.Println("========================================")
	log.Printf("  API:    http://localhost:%d", *apiPort)
	log.Printf("  Socket: %s", *socketPath)
	if *webuiDir != "" {
		log.Println("  Web UI: http://localhost:8000")
	}
	log.Println("========================================")
	log.Println("")
	log.Println("Sample domains created. Press Ctrl+C to stop.")

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	listener.Close()
}

func addSampleDomains(st *state.FakeState) {
	st.AddDomain(&state.Domain{
		Name:      "web-server-01",
		UUID:      [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		ID:        1,
		State:     state.DomainRunning,
		MaxMem:    4194304,
		Memory:    2097152,
		NrVirtCPU: 4,
	})

	st.AddDomain(&state.Domain{
		Name:      "database-01",
		UUID:      [16]byte{2, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		ID:        2,
		State:     state.DomainRunning,
		MaxMem:    8388608,
		Memory:    8388608,
		NrVirtCPU: 8,
	})

	st.AddDomain(&state.Domain{
		Name:      "worker-01",
		UUID:      [16]byte{3, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		ID:        -1,
		State:     state.DomainShutoff,
		MaxMem:    2097152,
		Memory:    1048576,
		NrVirtCPU: 2,
	})

	log.Printf("Added %d sample domains", 3)
}

func acceptConnections(listener net.Listener, st *state.FakeState) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		log.Printf("New connection from %s", conn.RemoteAddr())
		// Note: In demo mode we don't handle libvirt protocol, just accept connections
		// The full protocol handling is in the main fake-libvirtd command
		go func(c net.Conn) {
			buf := make([]byte, 1024)
			for {
				_, err := c.Read(buf)
				if err != nil {
					c.Close()
					return
				}
			}
		}(conn)
	}
}
