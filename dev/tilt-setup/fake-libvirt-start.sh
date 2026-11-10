#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
#
# SPDX-License-Identifier: Apache-2.0

# Starts fake-libvirtd on the host at /tmp/fake-libvirt/libvirt.sock.
# Tilt runs this as a local_resource so it restarts on source changes.
set -euo pipefail

SOCKET_DIR=/tmp/fake-libvirt
SOCKET_PATH=$SOCKET_DIR/libvirt.sock

mkdir -p "$SOCKET_DIR"
rm -f "$SOCKET_PATH"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

mkdir -p "$SOCKET_DIR"
rm -f "$SOCKET_PATH"

cd "$REPO_ROOT/dev/fake-libvirt"
exec go run ./cmd/fake-libvirtd/ --socket "$SOCKET_PATH"
