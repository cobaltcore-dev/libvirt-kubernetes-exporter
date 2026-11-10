# SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
#
# SPDX-License-Identifier: Apache-2.0

ARG IMAGE=golang:1.26
ARG RUNTIME_IMAGE=gcr.io/distroless/static:nonroot
ARG RUN_AS_USER=42438:42438

FROM $IMAGE AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . ./

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /libvirt-exporter ./cmd/operator

FROM $RUNTIME_IMAGE

ARG RUN_AS_USER=42438:42438

COPY --from=builder /libvirt-exporter /usr/local/bin/libvirt-exporter

USER $RUN_AS_USER

ENTRYPOINT ["/usr/local/bin/libvirt-exporter"]
