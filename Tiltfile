# SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

# Tiltfile for local development with fake-libvirt and kind
#
# Usage:
#   tilt up

# --- Cluster ---

# Use a local kind cluster. Create it with:
#   kind create cluster --name libvirt-dev
# Tilt will connect to whichever kubecontext is currently active.
allow_k8s_contexts("kind-libvirt-dev")

# --- Images ---

load("ext://helm_resource", "helm_resource", "helm_repo")

# fake-libvirtd sidecar
docker_build(
    "fake-libvirtd-dev",
    context="dev/fake-libvirt",
    dockerfile="dev/fake-libvirt/Dockerfile",
)

# operator with live rebuild
docker_build(
    "libvirt-exporter-dev",
    context=".",
    dockerfile="Dockerfile",
    build_args={"RUN_AS_USER": "0"},
    live_update=[
        sync("./", "/app/"),
        run("cd /app && CGO_ENABLED=0 go build -o /usr/local/bin/libvirt-exporter ./cmd/operator"),
    ],
)

# --- Kubernetes resources ---

helm_resource(
    "libvirt-exporter-dev",
    "charts/libvirt-kubernetes-exporter",
    namespace="libvirt-kubernetes-exporter",
    flags=["--create-namespace", "--values=dev/tilt-setup/values-dev.yaml"],
    image_deps=["libvirt-exporter-dev", "fake-libvirtd-dev"],
    image_keys=[
        ("image.repository", "image.tag"),
        ("fakeLibvirt.repository", "fakeLibvirt.tag"),
    ],
    labels=["operator"],
    port_forwards=["8081:8081"],
)
