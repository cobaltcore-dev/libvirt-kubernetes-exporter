// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

// Package domainxml extracts selected data from libvirt domain XML.
package domainxml

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/types"
)

type domainEnvelope struct {
	Metadata struct {
		Inner string `xml:",innerxml"`
	} `xml:"metadata"`
}

type novaInstance struct {
	Name   string     `xml:"name"`
	Flavor novaFlavor `xml:"flavor"`
	Owner  novaOwner  `xml:"owner"`
}

type novaFlavor struct {
	Name string `xml:"name,attr"`
}

type novaOwner struct {
	User    novaText `xml:"user"`
	Project novaText `xml:"project"`
}

type novaText struct {
	Value string `xml:",chardata"`
}

// ParseNovaMetadata extracts the small set of Nova fields we expose on CRDs.
func ParseNovaMetadata(domainXML string) types.NovaMetadata {
	var envelope domainEnvelope
	if err := xml.Unmarshal([]byte(domainXML), &envelope); err != nil {
		return types.NovaMetadata{}
	}

	decoder := xml.NewDecoder(strings.NewReader(envelope.Metadata.Inner))
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return types.NovaMetadata{}
		}
		if err != nil {
			return types.NovaMetadata{}
		}

		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "instance" {
			continue
		}

		var instance novaInstance
		if err := decoder.DecodeElement(&instance, &start); err != nil {
			return types.NovaMetadata{}
		}
		return types.NovaMetadata{
			Name:    strings.TrimSpace(instance.Name),
			Flavor:  strings.TrimSpace(instance.Flavor.Name),
			Project: strings.TrimSpace(instance.Owner.Project.Value),
			User:    strings.TrimSpace(instance.Owner.User.Value),
		}
	}
}
