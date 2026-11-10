// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"log"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/rpc"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/fake-libvirt/internal/state"
)

const errNoDomain = 42 // VIR_ERR_NO_DOMAIN

// DOMAIN_GET_MESSAGES (proc 426): return empty message list.
func handleDomainGetMessages(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Uint32(0) // empty []string
	return rpc.Reply(msg, enc.Bytes())
}

// DOMAIN_GET_OS_TYPE (proc 19): return "hvm" for all domains.
func handleDomainGetOsType(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.String("hvm")
	return rpc.Reply(msg, enc.Bytes())
}

// DOMAIN_GET_AUTOSTART (proc 15): return 0 (disabled).
func handleDomainGetAutostart(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(0)
	return rpc.Reply(msg, enc.Bytes())
}

// DOMAIN_IS_PERSISTENT (proc 151): return 1 (persistent).
func handleDomainIsPersistent(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(1)
	return rpc.Reply(msg, enc.Bytes())
}

// DOMAIN_HAS_MANAGED_SAVE_IMAGE (proc 183): return 0 (no managed save).
func handleDomainHasManagedSave(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(0)
	return rpc.Reply(msg, enc.Bytes())
}

// DOMAIN_GET_AUTOSTART_ONCE (proc 449): return 0 (disabled).
func handleDomainGetAutostartOnce(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(0)
	return rpc.Reply(msg, enc.Bytes())
}

// NODE_GET_SECURITY_MODEL (proc 122): return empty model and doi (no SELinux/AppArmor).
func handleNodeGetSecurityModel(msg *rpc.Message) *rpc.Message {
	// Model and Doi are []int8 encoded as opaque byte arrays (uint32 len + bytes + padding).
	var enc rpc.XDREncoder
	enc.Uint32(0) // model: empty
	enc.Uint32(0) // doi: empty
	return rpc.Reply(msg, enc.Bytes())
}

// CONNECT_SUPPORTS_FEATURE (proc 60): return 0 (not supported) for all features.
func handleConnectSupportsFeature(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Int32(0)
	return rpc.Reply(msg, enc.Bytes())
}

// DOMAIN_LOOKUP_BY_NAME (proc 23): find a domain by name, return Domain struct.
func handleDomainLookupByName(msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	name, err := dec.String()
	if err != nil {
		log.Printf("DomainLookupByName: decode error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}

	domain := fakeState.GetDomainByName(name)
	if domain == nil {
		log.Printf("DomainLookupByName: not found: %q", name)
		return rpc.ErrorReplyWithMsg(msg, errNoDomain, fmt.Sprintf("Domain not found: no domain with matching name '%s'", name))
	}

	var enc rpc.XDREncoder
	enc.String(domain.Name)
	enc.FixedBytes(domain.UUID[:])
	enc.Int32(domain.ID)
	return rpc.Reply(msg, enc.Bytes())
}

// AUTH_LIST (proc 66): return empty auth list (no authentication required).
func handleAuthList(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Uint32(0) // empty array of auth types
	return rpc.Reply(msg, enc.Bytes())
}

// CONNECT_OPEN (proc 1): decode URI, accept any valid URI.
func handleConnectOpen(conn *connState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	uris, err := dec.OptString()
	if err == nil && len(uris) > 0 {
		conn.uri = uris[0]
		log.Printf("ConnectOpen: URI=%q", conn.uri)
	}
	return rpc.Reply(msg, nil)
}

// CONNECT_CLOSE (proc 2): acknowledge, cleanup is handled by socket close.
func handleConnectClose(msg *rpc.Message) *rpc.Message {
	return rpc.Reply(msg, nil)
}

// CONNECT_GET_VERSION (proc 4): return hypervisor version 9002000 (QEMU 9.2.0).
func handleConnectGetVersion(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Uint64(9002000)
	return rpc.Reply(msg, enc.Bytes())
}

// CONNECT_GET_URI (proc 110): return stored connection URI.
func handleConnectGetUri(conn *connState, msg *rpc.Message) *rpc.Message {
	uri := conn.uri
	if uri == "" {
		uri = "qemu:///system"
	}
	var enc rpc.XDREncoder
	enc.String(uri)
	return rpc.Reply(msg, enc.Bytes())
}

// CONNECT_GET_LIB_VERSION (proc 157): return libvirt version 10001000 (10.1.0).
func handleConnectGetLibVersion(msg *rpc.Message) *rpc.Message {
	var enc rpc.XDREncoder
	enc.Uint64(10001000)
	return rpc.Reply(msg, enc.Bytes())
}

// CONNECT_LIST_ALL_DOMAINS (proc 273): return all domains, optionally filtered.
func handleConnectListAllDomains(msg *rpc.Message) *rpc.Message {
	// Decode args: NeedResults (int32), Flags (uint32).
	dec := rpc.NewXDRDecoder(msg.Payload)
	needResults, err := dec.Int32()
	if err != nil {
		log.Printf("ConnectListAllDomains: decode error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	flags, err := dec.Uint32()
	if err != nil {
		log.Printf("ConnectListAllDomains: decode flags error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	_ = flags // filtering by flags (active/inactive) not yet implemented

	domains := fakeState.ListDomains()
	log.Printf("ConnectListAllDomains: needResults=%d flags=%d domains=%d", needResults, flags, len(domains))
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

// DOMAIN_GET_STATE (proc 212): return state and reason for a domain.
func handleDomainGetState(msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	_, err := dec.String() // Name
	if err != nil {
		log.Printf("DomainGetState: decode name error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	uuidBytes, err := dec.FixedBytes(16)
	if err != nil {
		log.Printf("DomainGetState: decode uuid error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	var uuid [16]byte
	copy(uuid[:], uuidBytes)

	domain, ok := fakeState.GetDomain(uuid)
	if !ok {
		log.Printf("DomainGetState: domain not found: %x", uuid)
		return rpc.ErrorReplyWithMsg(msg, errNoDomain, fmt.Sprintf("Domain not found: no domain with matching uuid '%x'", uuid))
	}

	var enc rpc.XDREncoder
	enc.Int32(int32(domain.State))
	enc.Int32(domain.StateReason)
	return rpc.Reply(msg, enc.Bytes())
}

// DOMAIN_GET_INFO (proc 16): return state/memory/vcpu/cputime for a domain.
func handleDomainGetInfo(msg *rpc.Message) *rpc.Message {
	// Decode Domain arg: Name (string), UUID ([16]byte), ID (int32).
	dec := rpc.NewXDRDecoder(msg.Payload)
	_, err := dec.String() // Name
	if err != nil {
		log.Printf("DomainGetInfo: decode name error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	uuidBytes, err := dec.FixedBytes(16)
	if err != nil {
		log.Printf("DomainGetInfo: decode uuid error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	var uuid [16]byte
	copy(uuid[:], uuidBytes)

	domain, ok := fakeState.GetDomain(uuid)
	if !ok {
		log.Printf("DomainGetInfo: domain not found: %s", fmt.Sprintf("%x", uuid))
		return rpc.ErrorReplyWithMsg(msg, errNoDomain, fmt.Sprintf("Domain not found: no domain with matching uuid '%x'", uuid))
	}

	var enc rpc.XDREncoder
	enc.Uint32(uint32(domain.State)) // uint8 → uint32 in XDR
	enc.Uint64(domain.MaxMem)
	enc.Uint64(domain.Memory)
	enc.Uint32(uint32(domain.NrVirtCPU)) // uint16 → uint32 in XDR
	enc.Uint64(domain.CPUTime)
	return rpc.Reply(msg, enc.Bytes())
}

// DOMAIN_GET_XML_DESC (proc 14): return minimal XML description for a domain.
func handleDomainGetXMLDesc(msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	_, err := dec.String() // Name
	if err != nil {
		log.Printf("DomainGetXMLDesc: decode name error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	uuidBytes, err := dec.FixedBytes(16)
	if err != nil {
		log.Printf("DomainGetXMLDesc: decode uuid error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	var uuid [16]byte
	copy(uuid[:], uuidBytes)

	domain, ok := fakeState.GetDomain(uuid)
	if !ok {
		log.Printf("DomainGetXMLDesc: domain not found: %x", uuid)
		return rpc.ErrorReplyWithMsg(msg, errNoDomain, fmt.Sprintf("Domain not found: no domain with matching uuid '%x'", uuid))
	}

	uuidStr := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		domain.UUID[0:4], domain.UUID[4:6], domain.UUID[6:8], domain.UUID[8:10], domain.UUID[10:16])
	xml := fmt.Sprintf(`<domain type='kvm'><name>%s</name><uuid>%s</uuid><memory unit='KiB'>%d</memory><currentMemory unit='KiB'>%d</currentMemory><vcpu>%d</vcpu><os><type arch='x86_64' machine='pc'>hvm</type></os></domain>`,
		domain.Name, uuidStr, domain.MaxMem, domain.Memory, domain.NrVirtCPU)

	var enc rpc.XDREncoder
	enc.String(xml)
	return rpc.Reply(msg, enc.Bytes())
}

// CONNECT_DOMAIN_EVENT_CALLBACK_REGISTER_ANY (proc 316): register for domain events.
func handleConnectDomainEventCallbackRegisterAny(conn *connState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	eventID, err := dec.Int32() // event type (0 = lifecycle)
	if err != nil {
		log.Printf("ConnectDomainEventCallbackRegisterAny: decode eventID error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	// Optional domain filter (we ignore it, accepting events for all domains)
	_, _ = dec.OptString() // domain name filter

	log.Printf("ConnectDomainEventCallbackRegisterAny: eventID=%d", eventID)

	// Register callback with state and wire up to connection's event channel
	callbackID := fakeState.RegisterEventCallback(eventID, conn.eventCh)
	conn.callbackIDs = append(conn.callbackIDs, callbackID)

	var enc rpc.XDREncoder
	enc.Int32(callbackID)
	return rpc.Reply(msg, enc.Bytes())
}

// CONNECT_DOMAIN_EVENT_CALLBACK_DEREGISTER_ANY (proc 317): deregister event callback.
func handleConnectDomainEventCallbackDeregisterAny(conn *connState, msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	callbackID, err := dec.Int32()
	if err != nil {
		log.Printf("ConnectDomainEventCallbackDeregisterAny: decode error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}

	log.Printf("ConnectDomainEventCallbackDeregisterAny: callbackID=%d", callbackID)

	// Remove from state
	if err := fakeState.DeregisterEventCallback(callbackID); err != nil {
		log.Printf("ConnectDomainEventCallbackDeregisterAny: %v", err)
	}

	// Remove from connection's tracking
	for i, cbID := range conn.callbackIDs {
		if cbID == callbackID {
			conn.callbackIDs = append(conn.callbackIDs[:i], conn.callbackIDs[i+1:]...)
			break
		}
	}

	return rpc.Reply(msg, nil)
}

// CONNECT_REGISTER_CLOSE_CALLBACK (proc 360): register for close notification.
func handleConnectRegisterCloseCallback(msg *rpc.Message) *rpc.Message {
	// We acknowledge but don't actually implement close callbacks
	return rpc.Reply(msg, nil)
}

// DOMAIN_GET_JOB_STATS (proc 298): return job type and typed params.
func handleDomainGetJobStats(msg *rpc.Message) *rpc.Message {
	dec := rpc.NewXDRDecoder(msg.Payload)
	_, _ = dec.String() // Name
	uuidBytes, err := dec.FixedBytes(16)
	if err != nil {
		log.Printf("DomainGetJobStats: decode uuid error: %v", err)
		return rpc.ErrorReply(msg, nil)
	}
	var uuid [16]byte
	copy(uuid[:], uuidBytes)

	domain, ok := fakeState.GetDomain(uuid)
	if !ok {
		return rpc.ErrorReplyWithMsg(msg, errNoDomain, fmt.Sprintf("Domain not found: no domain with matching uuid '%x'", uuid))
	}

	var enc rpc.XDREncoder
	enc.Int32(domain.JobType)

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

// ForceEvent allows the harness to inject a lifecycle event for testing.
func ForceEvent(domain *state.Domain, eventType, detail int32) {
	fakeState.PublishEvent(state.LifecycleEvent{
		Domain: domain,
		Event:  eventType,
		Detail: detail,
	})
}
