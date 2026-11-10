// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	HeaderSize = 24
	LengthSize = 4

	TypeCall    = 0
	TypeReply   = 1
	TypeMessage = 2
	TypeStream  = 3

	StatusOK       = 0
	StatusError    = 1
	StatusContinue = 2

	RemoteProgram = 0x20008086
	RemoteVersion = 1
)

type Message struct {
	Length    uint32
	Program   uint32
	Version   uint32
	Procedure int32
	Type      int32
	Serial    uint32
	Status    int32
	Payload   []byte
}

func ReadMessage(r io.Reader) (*Message, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	if length < LengthSize+HeaderSize {
		return nil, fmt.Errorf("message too short: %d bytes", length)
	}

	data := make([]byte, length-LengthSize)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	return &Message{
		Length:    length,
		Program:   binary.BigEndian.Uint32(data[0:4]),
		Version:   binary.BigEndian.Uint32(data[4:8]),
		Procedure: int32(binary.BigEndian.Uint32(data[8:12])),
		Type:      int32(binary.BigEndian.Uint32(data[12:16])),
		Serial:    binary.BigEndian.Uint32(data[16:20]),
		Status:    int32(binary.BigEndian.Uint32(data[20:24])),
		Payload:   data[24:],
	}, nil
}

func WriteMessage(w io.Writer, msg *Message) error {
	if err := binary.Write(w, binary.BigEndian, msg.Length); err != nil {
		return err
	}

	header := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(header[0:4], msg.Program)
	binary.BigEndian.PutUint32(header[4:8], msg.Version)
	binary.BigEndian.PutUint32(header[8:12], uint32(msg.Procedure))
	binary.BigEndian.PutUint32(header[12:16], uint32(msg.Type))
	binary.BigEndian.PutUint32(header[16:20], msg.Serial)
	binary.BigEndian.PutUint32(header[20:24], uint32(msg.Status))

	if _, err := w.Write(header); err != nil {
		return err
	}

	if len(msg.Payload) > 0 {
		if _, err := w.Write(msg.Payload); err != nil {
			return err
		}
	}

	return nil
}

func Reply(req *Message, payload []byte) *Message {
	totalLen := uint32(LengthSize + HeaderSize + len(payload))
	return &Message{
		Length:    totalLen,
		Program:   RemoteProgram,
		Version:   RemoteVersion,
		Procedure: req.Procedure,
		Type:      TypeReply,
		Serial:    req.Serial,
		Status:    StatusOK,
		Payload:   payload,
	}
}

func ErrorReplyWithMsg(req *Message, code uint32, message string) *Message {
	var enc XDREncoder
	enc.Uint32(code)   // Code
	enc.Uint32(0)      // DomainID (VIR_FROM_NONE)
	enc.Uint32(0)      // Padding (uint8 promoted to uint32)
	enc.String(message)
	enc.Uint32(2)      // Level (VIR_ERR_ERROR)
	payload := enc.Bytes()
	return &Message{
		Length:    uint32(LengthSize + HeaderSize + len(payload)),
		Program:   RemoteProgram,
		Version:   RemoteVersion,
		Procedure: req.Procedure,
		Type:      TypeReply,
		Serial:    req.Serial,
		Status:    StatusError,
		Payload:   payload,
	}
}

func ErrorReply(req *Message, payload []byte) *Message {
	totalLen := uint32(LengthSize + HeaderSize + len(payload))
	return &Message{
		Length:    totalLen,
		Program:   RemoteProgram,
		Version:   RemoteVersion,
		Procedure: req.Procedure,
		Type:      TypeReply,
		Serial:    req.Serial,
		Status:    StatusError,
		Payload:   payload,
	}
}
