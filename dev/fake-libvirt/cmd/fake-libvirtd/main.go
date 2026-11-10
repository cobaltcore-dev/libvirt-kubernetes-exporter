// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/api"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/state"
)

var (
	socketPath string
	apiPort    string
)

var fakeState *state.FakeState

func main() {
	flag.StringVar(&socketPath, "socket", "./fake-libvirt.sock", "Unix socket path to listen on")
	flag.StringVar(&apiPort, "api-port", "", "HTTP API port (e.g., '8081'); disabled if empty")
	flag.Parse()

	fakeState = state.New()

	// Start API server if enabled
	if apiPort != "" {
		apiServer := api.NewServer(fakeState)
		go func() {
			addr := ":" + apiPort
			if err := apiServer.ListenAndServe(addr); err != nil {
				log.Printf("[api] server error: %v", err)
			}
		}()
	}

	// Start libvirt socket server
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", socketPath, err)
	}
	defer os.Remove(socketPath)

	log.Printf("fake-libvirtd listening on %s", socketPath)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				break
			}
			log.Printf("Accept error: %v", err)
			continue
		}
		log.Printf("New connection from %s", conn.RemoteAddr())
		go handleConnection(conn)
	}
}
