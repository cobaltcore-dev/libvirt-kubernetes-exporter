// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package domainxml

import "testing"

func TestParseNovaMetadata(t *testing.T) {
	meta := ParseNovaMetadata(`<domain type="kvm">
  <name>instance-8b416d1c</name>
  <metadata>
    <nova:instance xmlns:nova="http://openstack.org/xmlns/libvirt/nova/1.1">
      <nova:name>example-instance</nova:name>
      <nova:flavor name="m1.large">
        <nova:memory>491520</nova:memory>
      </nova:flavor>
      <nova:owner>
        <nova:user uuid="user-uuid">example-user</nova:user>
        <nova:project uuid="project-uuid">example-project</nova:project>
      </nova:owner>
    </nova:instance>
  </metadata>
</domain>`)

	if meta.Name != "example-instance" {
		t.Errorf("Name = %q", meta.Name)
	}
	if meta.Flavor != "m1.large" {
		t.Errorf("Flavor = %q", meta.Flavor)
	}
	if meta.Project != "example-project" {
		t.Errorf("Project = %q", meta.Project)
	}
	if meta.User != "example-user" {
		t.Errorf("User = %q", meta.User)
	}
}

func TestParseNovaMetadataNoMetadata(t *testing.T) {
	meta := ParseNovaMetadata(`<domain type="kvm"><name>test</name></domain>`)
	if !meta.IsZero() {
		t.Fatalf("expected empty metadata, got %+v", meta)
	}
}
