// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package fakelibvirt provides a fake libvirt daemon for testing.
//
// It can be used as:
// 1. A standalone daemon (cmd/fake-libvirtd)
// 2. A library for embedding in test harnesses
package fakelibvirt

import (
	"io"
	"log"
	"net"
	"sync"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/api"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/rpc"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/state"
)

// Server is a fake libvirt daemon.
type Server struct {
	state    *state.FakeState
	listener net.Listener
	wg       sync.WaitGroup
	closed   bool
	closeMu  sync.Mutex

	// Track active connections for cleanup
	connsMu sync.Mutex
	conns   []net.Conn
}

// NewServer creates a new fake libvirt server with the given state.
// If state is nil, a default state with test domains is created.
func NewServer(st *state.FakeState) *Server {
	if st == nil {
		st = state.New()
	}
	return &Server{state: st}
}

// State returns the server's state for manipulation.
func (s *Server) State() *state.FakeState {
	return s.state
}

// Listen starts listening on the given Unix socket path.
func (s *Server) Listen(socketPath string) error {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	s.listener = listener

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				s.closeMu.Lock()
				closed := s.closed
				s.closeMu.Unlock()
				if closed {
					return
				}
				log.Printf("Accept error: %v", err)
				continue
			}
			s.connsMu.Lock()
			s.conns = append(s.conns, conn)
			s.connsMu.Unlock()

			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				defer func() {
					s.connsMu.Lock()
					for i, cc := range s.conns {
						if cc == c {
							s.conns = append(s.conns[:i], s.conns[i+1:]...)
							break
						}
					}
					s.connsMu.Unlock()
				}()
				s.handleConnection(c)
			}(conn)
		}
	}()

	return nil
}

// Close stops the server.
func (s *Server) Close() error {
	s.closeMu.Lock()
	s.closed = true
	s.closeMu.Unlock()

	if s.listener != nil {
		s.listener.Close()
	}

	// Close all active connections to unblock readers
	s.connsMu.Lock()
	for _, c := range s.conns {
		c.Close()
	}
	s.conns = nil
	s.connsMu.Unlock()

	s.wg.Wait()
	return nil
}

// connState holds per-connection state.
type connState struct {
	server      *Server
	uri         string
	callbackIDs []int32
	eventCh     chan state.LifecycleEvent
	conn        net.Conn
	writeMu     sync.Mutex
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

func (s *Server) handleConnection(conn net.Conn) {
	cs := &connState{
		server:  s,
		conn:    conn,
		eventCh: make(chan state.LifecycleEvent, 100),
	}
	defer func() {
		cs.setClosed()
		conn.Close()
		close(cs.eventCh)
		for _, cbID := range cs.callbackIDs {
			s.state.DeregisterEventCallback(cbID)
		}
	}()

	go cs.eventPusher()

	for {
		msg, err := rpc.ReadMessage(conn)
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
			}
			return
		}

		resp := cs.dispatch(msg)
		cs.writeMu.Lock()
		err = rpc.WriteMessage(conn, resp)
		cs.writeMu.Unlock()
		if err != nil {
			log.Printf("Write error: %v", err)
			return
		}
	}
}

func (c *connState) eventPusher() {
	serial := uint32(1)
	for event := range c.eventCh {
		if c.isClosed() {
			return
		}

		var callbackID int32 = -1
		for _, cbID := range c.callbackIDs {
			callbackID = cbID
			break
		}
		if callbackID < 0 {
			continue
		}

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

func (c *connState) dispatch(msg *rpc.Message) *rpc.Message {
	st := c.server.state

	switch msg.Procedure {
	case rpc.ProcAuthList:
		return c.handleAuthList(msg)
	case rpc.ProcConnectOpen:
		return c.handleConnectOpen(msg)
	case rpc.ProcConnectClose:
		return c.handleConnectClose(msg)
	case rpc.ProcConnectGetVersion:
		return c.handleConnectGetVersion(msg)
	case rpc.ProcConnectGetUri:
		return c.handleConnectGetUri(msg)
	case rpc.ProcConnectGetLibVersion:
		return c.handleConnectGetLibVersion(msg)
	case rpc.ProcConnectListAllDomains:
		return c.handleConnectListAllDomains(st, msg)
	case rpc.ProcDomainGetInfo:
		return c.handleDomainGetInfo(st, msg)
	case rpc.ProcDomainGetState:
		return c.handleDomainGetState(st, msg)
	case rpc.ProcDomainGetJobStats:
		return c.handleDomainGetJobStats(st, msg)
	case rpc.ProcConnectSupportsFeature:
		return c.handleConnectSupportsFeature(msg)
	case rpc.ProcConnectDomainEventCallbackRegisterAny:
		return c.handleConnectDomainEventCallbackRegisterAny(st, msg)
	case rpc.ProcConnectDomainEventCallbackDeregisterAny:
		return c.handleConnectDomainEventCallbackDeregisterAny(st, msg)
	case rpc.ProcConnectRegisterCloseCallback:
		return c.handleConnectRegisterCloseCallback(msg)
	case rpc.ProcDomainLookupByName:
		return c.handleDomainLookupByName(st, msg)
	case rpc.ProcDomainGetOsType:
		return c.handleDomainGetOsType(msg)
	case rpc.ProcDomainGetXMLDesc:
		return c.handleDomainGetXMLDesc(msg)
	case rpc.ProcDomainGetAutostart:
		return c.handleDomainGetAutostart(msg)
	case rpc.ProcDomainIsPersistent:
		return c.handleDomainIsPersistent(msg)
	case rpc.ProcDomainHasManagedSave:
		return c.handleDomainHasManagedSave(msg)
	case rpc.ProcNodeGetSecurityModel:
		return c.handleNodeGetSecurityModel(msg)
	default:
		log.Printf("Unhandled procedure %d", msg.Procedure)
		return rpc.ErrorReply(msg, nil)
	}
}

// Handler implementations

func (c *connState) handleDomainGetJobStats(st *state.FakeState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	_, _ = dec.String()
	uuidBytes, _ := dec.FixedBytes(16)
	var uuid [16]byte
	copy(uuid[:], uuidBytes)

	domain, ok := st.GetDomain(uuid)
	if !ok {
		return rpc.ErrorReplyWithMsg(msg, 42, "Domain not found")
	}

	var enc rpc.XDREncoder
	enc.Int32(domain.JobType) // rType

	// Count non-zero params to encode
	type param struct {
		name  string
		isInt bool
		iVal  int32
	}
	var params []param
	if domain.JobType != 0 {
		if domain.JobOperation != 0 {
			params = append(params, param{name: "operation", isInt: true, iVal: domain.JobOperation})
		}
	}

	enc.Uint32(uint32(len(params)))
	for _, p := range params {
		enc.TypedParamInt32(p.name, p.iVal)
	}
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleAuthList(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Uint32(1) // auth types count
	enc.Uint32(0) // REMOTE_AUTH_NONE
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleConnectOpen(msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	uris, _ := dec.OptString()
	if len(uris) > 0 {
		c.uri = uris[0]
	}
	return rpc.Reply(msg, nil)
}

func (c *connState) handleConnectClose(msg *rpc.Message) *rpc.Message {
	return rpc.Reply(msg, nil)
}

func (c *connState) handleConnectGetVersion(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Uint64(9002000) // QEMU 9.2.0
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleConnectGetUri(msg *rpc.Message) *rpc.Message {
	uri := c.uri
	if uri == "" {
		uri = "qemu:///system"
	}
	var enc rpc.XDREncoder
	enc.String(uri)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleConnectGetLibVersion(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Uint64(10001000) // libvirt 10.1.0
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleConnectListAllDomains(st *state.FakeState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	needResults, _ := dec.Int32()
	_, _ = dec.Uint32() // flags

	domains := st.ListDomains()
	var enc rpc.XDREncoder
	if needResults != 0 {
		enc.Uint32(uint32(len(domains)))
		for _, d := range domains {
			enc.String(d.Name)
			enc.FixedBytes(d.UUID[:])
			enc.Int32(d.ID)
		}
	} else {
		enc.Uint32(0)
	}
	enc.Uint32(uint32(len(domains)))
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleDomainGetInfo(st *state.FakeState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	_, _ = dec.String()
	uuidBytes, _ := dec.FixedBytes(16)
	var uuid [16]byte
	copy(uuid[:], uuidBytes)

	domain, ok := st.GetDomain(uuid)
	if !ok {
		return rpc.ErrorReplyWithMsg(msg, 42, "Domain not found")
	}

	var enc rpc.XDREncoder
	enc.Uint32(uint32(domain.State))
	enc.Uint64(domain.MaxMem)
	enc.Uint64(domain.Memory)
	enc.Uint32(uint32(domain.NrVirtCPU))
	enc.Uint64(domain.CPUTime)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleDomainGetState(st *state.FakeState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	_, _ = dec.String()
	uuidBytes, _ := dec.FixedBytes(16)
	var uuid [16]byte
	copy(uuid[:], uuidBytes)

	domain, ok := st.GetDomain(uuid)
	if !ok {
		return rpc.ErrorReplyWithMsg(msg, 42, "Domain not found")
	}

	var enc rpc.XDREncoder
	enc.Int32(int32(domain.State))
	enc.Int32(domain.StateReason)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleDomainGetXMLDesc(msg *rpc.Message) *rpc.Message {
	// Return a minimal valid XML for the domain
	// In a real implementation, this would return the full domain XML
	xml := `<domain type='kvm'><name>fake-domain</name><uuid>00000000-0000-0000-0000-000000000000</uuid><memory>1048576</memory><vcpu>1</vcpu></domain>`
	var enc rpc.XDREncoder
	enc.String(xml)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleConnectSupportsFeature(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(1) // support all features
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleConnectDomainEventCallbackRegisterAny(st *state.FakeState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	eventID, _ := dec.Int32()

	callbackID := st.RegisterEventCallback(eventID, c.eventCh)
	c.callbackIDs = append(c.callbackIDs, callbackID)

	var enc rpc.XDREncoder
	enc.Int32(callbackID)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleConnectDomainEventCallbackDeregisterAny(st *state.FakeState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	callbackID, _ := dec.Int32()

	st.DeregisterEventCallback(callbackID)
	for i, cbID := range c.callbackIDs {
		if cbID == callbackID {
			c.callbackIDs = append(c.callbackIDs[:i], c.callbackIDs[i+1:]...)
			break
		}
	}

	return rpc.Reply(msg, nil)
}

func (c *connState) handleConnectRegisterCloseCallback(msg *rpc.Message) *rpc.Message {
	return rpc.Reply(msg, nil)
}

func (c *connState) handleDomainLookupByName(st *state.FakeState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	name, _ := dec.String()

	domain := st.GetDomainByName(name)
	if domain == nil {
		return rpc.ErrorReplyWithMsg(msg, 42, "Domain not found: "+name)
	}

	var enc rpc.XDREncoder
	enc.String(domain.Name)
	enc.FixedBytes(domain.UUID[:])
	enc.Int32(domain.ID)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleDomainGetOsType(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.String("hvm")
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleDomainGetAutostart(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(0)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleDomainIsPersistent(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(1)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleDomainHasManagedSave(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(0)
	return rpc.Reply(msg, enc.Bytes())
}

func (c *connState) handleNodeGetSecurityModel(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Uint32(0) // model: empty
	enc.Uint32(0) // doi: empty
	return rpc.Reply(msg, enc.Bytes())
}

// Re-export state types for convenience
type (
	FakeState      = state.FakeState
	Domain         = state.Domain
	LifecycleEvent = state.LifecycleEvent
)

// Re-export state constants
const (
	DomainNostate     = state.DomainNostate
	DomainRunning     = state.DomainRunning
	DomainBlocked     = state.DomainBlocked
	DomainPaused      = state.DomainPaused
	DomainShutdown    = state.DomainShutdown
	DomainShutoff     = state.DomainShutoff
	DomainCrashed     = state.DomainCrashed
	DomainPmsuspended = state.DomainPmsuspended

	DomainEventDefined   = state.DomainEventDefined
	DomainEventUndefined = state.DomainEventUndefined
	DomainEventStarted   = state.DomainEventStarted
	DomainEventSuspended = state.DomainEventSuspended
	DomainEventResumed   = state.DomainEventResumed
	DomainEventStopped   = state.DomainEventStopped
	DomainEventShutdown  = state.DomainEventShutdown
	DomainEventCrashed   = state.DomainEventCrashed
)

// NewEmpty creates empty state (for tests that want to start fresh).
func NewEmpty() *state.FakeState {
	return state.NewEmpty()
}

// NewStateWithDomains creates state with initial domains.
func NewStateWithDomains(domains []*state.Domain) *state.FakeState {
	st := state.NewEmpty()
	for _, d := range domains {
		st.AddDomain(d)
	}
	return st
}

// APIServer wraps the REST API server.
type APIServer struct {
	server *api.Server
}

// NewAPIServer creates a new API server for the given state.
// This provides external access to the API without importing internal packages.
func NewAPIServer(st *FakeState) *APIServer {
	return &APIServer{server: api.NewServer(st)}
}

// ListenAndServe starts the HTTP server.
func (s *APIServer) ListenAndServe(addr string) error {
	return s.server.ListenAndServe(addr)
}

// Close shuts down the server.
func (s *APIServer) Close() error {
	return s.server.Close()
}
