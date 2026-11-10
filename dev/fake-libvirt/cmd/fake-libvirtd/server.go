// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"log"
	"net"
	"sync"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/rpc"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/state"
)

// connState holds per-connection state.
type connState struct {
	uri         string
	callbackIDs []int32 // registered callback IDs for cleanup
	eventCh     chan state.LifecycleEvent
	conn        net.Conn
	writeMu     sync.Mutex // protects concurrent writes to conn
	closed      bool
	closeMu     sync.RWMutex
}

func (c *connState) isClosed() bool {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	return c.closed
}

func (c *connState) setClosed() {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	c.closed = true
}

func handleConnection(conn net.Conn) {
	cs := &connState{
		conn:    conn,
		eventCh: make(chan state.LifecycleEvent, 100),
	}
	defer func() {
		cs.setClosed()
		conn.Close()
		close(cs.eventCh)
		// Cleanup event subscriptions
		for _, cbID := range cs.callbackIDs {
			fakeState.DeregisterEventCallback(cbID)
		}
	}()

	// Start event pusher goroutine
	go cs.eventPusher()

	// Main request loop
	for {
		msg, err := rpc.ReadMessage(conn)
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
			}
			return
		}

		resp := dispatch(cs, msg)
		cs.writeMu.Lock()
		err = rpc.WriteMessage(conn, resp)
		cs.writeMu.Unlock()
		if err != nil {
			log.Printf("Write error: %v", err)
			return
		}
	}
}

// eventPusher sends lifecycle events to the client asynchronously.
func (c *connState) eventPusher() {
	serial := uint32(1) // events use their own serial sequence
	for event := range c.eventCh {
		if c.isClosed() {
			return
		}

		// Find the callback ID for lifecycle events (event type 0)
		var callbackID int32 = -1
		for _, cbID := range c.callbackIDs {
			// For simplicity, use the first registered callback
			// In a real implementation, we'd match by event type
			callbackID = cbID
			break
		}
		if callbackID < 0 {
			continue // no callback registered
		}

		// Build lifecycle event message
		msg := buildLifecycleEventMessage(serial, callbackID, event)
		serial++

		c.writeMu.Lock()
		err := rpc.WriteMessage(c.conn, msg)
		c.writeMu.Unlock()
		if err != nil {
			log.Printf("Event push error: %v", err)
			return
		}
	}
}

// buildLifecycleEventMessage creates a DOMAIN_EVENT_CALLBACK_LIFECYCLE message.
func buildLifecycleEventMessage(serial uint32, callbackID int32, event state.LifecycleEvent) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(callbackID)
	enc.String(event.Domain.Name)
	enc.FixedBytes(event.Domain.UUID[:])
	enc.Int32(event.Domain.ID)
	enc.Int32(event.Event)
	enc.Int32(event.Detail)

	payload := enc.Bytes()
	return &rpc.Message{
		Length:    uint32(rpc.LengthSize + rpc.HeaderSize + len(payload)),
		Program:   rpc.RemoteProgram,
		Version:   rpc.RemoteVersion,
		Procedure: rpc.ProcDomainEventCallbackLifecycle,
		Type:      rpc.TypeMessage,
		Serial:    serial,
		Status:    rpc.StatusOK,
		Payload:   payload,
	}
}

func dispatch(conn *connState, msg *rpc.Message) *rpc.Message {
	switch msg.Procedure {
	case rpc.ProcDomainGetMessages:
		return handleDomainGetMessages(msg)
	case rpc.ProcDomainGetAutostart:
		return handleDomainGetAutostart(msg)
	case rpc.ProcDomainGetOsType:
		return handleDomainGetOsType(msg)
	case rpc.ProcDomainIsPersistent:
		return handleDomainIsPersistent(msg)
	case rpc.ProcDomainHasManagedSave:
		return handleDomainHasManagedSave(msg)
	case rpc.ProcDomainGetAutostartOnce:
		return handleDomainGetAutostartOnce(msg)
	case rpc.ProcNodeGetSecurityModel:
		return handleNodeGetSecurityModel(msg)
	case rpc.ProcConnectSupportsFeature:
		return handleConnectSupportsFeature(msg)
	case rpc.ProcDomainLookupByName:
		return handleDomainLookupByName(msg)
	case rpc.ProcAuthList:
		return handleAuthList(msg)
	case rpc.ProcConnectOpen:
		return handleConnectOpen(conn, msg)
	case rpc.ProcConnectClose:
		return handleConnectClose(msg)
	case rpc.ProcConnectGetVersion:
		return handleConnectGetVersion(msg)
	case rpc.ProcConnectGetUri:
		return handleConnectGetUri(conn, msg)
	case rpc.ProcConnectGetLibVersion:
		return handleConnectGetLibVersion(msg)
	case rpc.ProcConnectListAllDomains:
		return handleConnectListAllDomains(msg)
	case rpc.ProcDomainGetJobStats:
		return handleDomainGetJobStats(msg)
	case rpc.ProcDomainGetState:
		return handleDomainGetState(msg)
	case rpc.ProcDomainGetInfo:
		return handleDomainGetInfo(msg)
	case rpc.ProcDomainGetXMLDesc:
		return handleDomainGetXMLDesc(msg)
	case rpc.ProcConnectDomainEventCallbackRegisterAny:
		return handleConnectDomainEventCallbackRegisterAny(conn, msg)
	case rpc.ProcConnectDomainEventCallbackDeregisterAny:
		return handleConnectDomainEventCallbackDeregisterAny(conn, msg)
	case rpc.ProcConnectRegisterCloseCallback:
		return handleConnectRegisterCloseCallback(msg)
	default:
		log.Printf("Unhandled procedure %d", msg.Procedure)
		return rpc.ErrorReply(msg, nil)
	}
}
