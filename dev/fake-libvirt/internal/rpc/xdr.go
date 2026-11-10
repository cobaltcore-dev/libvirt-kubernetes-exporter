// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package rpc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// XDREncoder writes XDR-encoded values into a buffer.
type XDREncoder struct {
	buf bytes.Buffer
}

func (e *XDREncoder) Uint32(v uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	e.buf.Write(b[:])
}

func (e *XDREncoder) Int32(v int32) {
	e.Uint32(uint32(v))
}

func (e *XDREncoder) Uint64(v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	e.buf.Write(b[:])
}

// String encodes a variable-length opaque string: uint32 length + bytes + padding to 4-byte boundary.
func (e *XDREncoder) String(s string) {
	e.Uint32(uint32(len(s)))
	e.buf.WriteString(s)
	if pad := (4 - len(s)%4) % 4; pad > 0 {
		e.buf.Write(make([]byte, pad))
	}
}

// FixedBytes encodes a fixed-length byte array + padding to 4-byte boundary.
func (e *XDREncoder) FixedBytes(b []byte) {
	e.buf.Write(b)
	if pad := (4 - len(b)%4) % 4; pad > 0 {
		e.buf.Write(make([]byte, pad))
	}
}

// OptString encodes an OptString ([]string as XDR variable-length array).
func (e *XDREncoder) OptString(ss []string) {
	e.Uint32(uint32(len(ss)))
	for _, s := range ss {
		e.String(s)
	}
}

// TypedParamInt32 encodes a TypedParam with an int32 value (discriminant=1).
func (e *XDREncoder) TypedParamInt32(name string, value int32) {
	e.String(name)
	e.Uint32(1) // TypedParamInt discriminant
	e.Int32(value)
}

func (e *XDREncoder) Bytes() []byte {
	return e.buf.Bytes()
}

// XDRDecoder reads XDR-encoded values from a reader.
type XDRDecoder struct {
	r io.Reader
}

func NewXDRDecoder(payload []byte) *XDRDecoder {
	return &XDRDecoder{r: bytes.NewReader(payload)}
}

func (d *XDRDecoder) Uint32() (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(d.r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func (d *XDRDecoder) Int32() (int32, error) {
	v, err := d.Uint32()
	return int32(v), err
}

func (d *XDRDecoder) String() (string, error) {
	length, err := d.Uint32()
	if err != nil {
		return "", err
	}
	if length > 4096 {
		return "", fmt.Errorf("string too long: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return "", err
	}
	if pad := (4 - int(length)%4) % 4; pad > 0 {
		discard := make([]byte, pad)
		if _, err := io.ReadFull(d.r, discard); err != nil {
			return "", err
		}
	}
	return string(buf), nil
}

// OptString decodes an OptString (variable-length array of strings).
func (d *XDRDecoder) OptString() ([]string, error) {
	count, err := d.Uint32()
	if err != nil {
		return nil, err
	}
	result := make([]string, count)
	for i := range result {
		if result[i], err = d.String(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// FixedBytes reads n bytes and discards padding to 4-byte boundary.
func (d *XDRDecoder) FixedBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return nil, err
	}
	if pad := (4 - n%4) % 4; pad > 0 {
		discard := make([]byte, pad)
		if _, err := io.ReadFull(d.r, discard); err != nil {
			return nil, err
		}
	}
	return buf, nil
}
