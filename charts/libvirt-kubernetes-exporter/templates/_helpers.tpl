# SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

{{/*
Expand the name of the chart.
*/}}
{{- define "libvirt-kubernetes-exporter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "libvirt-kubernetes-exporter.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "libvirt-kubernetes-exporter.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "libvirt-kubernetes-exporter.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "libvirt-kubernetes-exporter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "libvirt-kubernetes-exporter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Service account name
*/}}
{{- define "libvirt-kubernetes-exporter.serviceAccountName" -}}
{{- include "libvirt-kubernetes-exporter.fullname" . }}
{{- end }}

{{/*
Name for CLUSTER-SCOPED resources (ClusterRole, ClusterRoleBinding).

Cluster-scoped objects have no namespace, so unlike the namespaced Deployment/
ServiceAccount they would collide across releases installed on the same cluster
(multiple dev deployments + production). Key the name on the release namespace,
which is unique per deployment and already carries the app name (e.g.
libvirt-kubernetes-exporter-shared, or libvirt-kubernetes-exporter for prod).
*/}}
{{- define "libvirt-kubernetes-exporter.clusterScopedName" -}}
{{- .Release.Namespace | trunc 63 | trimSuffix "-" -}}
{{- end }}


{{/*
CRD plural resource names, incl. the optional crdSuffix. These MUST match the
base plurals baked into the CRD YAML by controller-gen (operator/pkg/crdmeta:
libvirtdomains / libvirtdomainheartbeats) plus the sed-applied suffix, so the
RBAC rules grant access to exactly the CRDs the operator uses. The suffix's
leading '-' is stripped to match the sed'd CRD name segment (e.g. "-shared" ->
"libvirtdomains2shared").
*/}}
{{- define "libvirt-kubernetes-exporter.crdSuffixClean" -}}
{{- .Values.crdSuffix | default "" | trimPrefix "-" -}}
{{- end }}
{{- define "libvirt-kubernetes-exporter.libvirtDomainResource" -}}
{{- printf "libvirtdomains%s" (include "libvirt-kubernetes-exporter.crdSuffixClean" .) -}}
{{- end }}
{{- define "libvirt-kubernetes-exporter.libvirtDomainHeartbeatResource" -}}
{{- printf "libvirtdomainheartbeats%s" (include "libvirt-kubernetes-exporter.crdSuffixClean" .) -}}
{{- end }}


{{/*
Validate required values
*/}}
{{- define "libvirt-kubernetes-exporter.validateValues" -}}
{{- if not .Values.image.repository -}}
{{- fail "image.repository is required — set it to your registry path, e.g. --set image.repository=registry.example.com/libvirt-kubernetes-exporter" -}}
{{- end -}}
{{- end }}
