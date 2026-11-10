# SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
#
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /fake-libvirtd ./cmd/fake-libvirtd

FROM alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1

COPY --from=builder /fake-libvirtd /usr/local/bin/fake-libvirtd

ENTRYPOINT ["/usr/local/bin/fake-libvirtd"]
