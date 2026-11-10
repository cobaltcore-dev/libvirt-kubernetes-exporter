#!/bin/bash


# Run the combined demo for the web UI
# Starts all API servers (fake-libvirt, fake-k8s, operator, harness) and the web UI
#
# Ports:
#   8000 - Web UI (static files)
#   8080 - Test harness API
#   8081 - fake-libvirt API
#   8082 - fake-k8s API
#   8083 - Operator API
#
# Usage:
#   ./dev/scripts/run-demo.sh              # Run demo
#   ./dev/scripts/run-demo.sh --build-only # Just build, don't run
#   ./dev/scripts/run-demo.sh --no-build   # Run without rebuilding

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"

BUILD=true
RUN=true

for arg in "$@"; do
    case $arg in
        --build-only)
            RUN=false
            ;;
        --no-build)
            BUILD=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --build-only  Build the demo binary but don't run it"
            echo "  --no-build    Run without rebuilding"
            echo "  --help        Show this help message"
            echo ""
            echo "Ports:"
            echo "  8000 - Web UI"
            echo "  8080 - Test harness API"
            echo "  8081 - fake-libvirt API"
            echo "  8082 - fake-k8s API"
            echo "  8083 - Operator API"
            exit 0
            ;;
    esac
done

if [ "$BUILD" = true ]; then
    echo "Building combined demo..."
    cd "$ROOT_DIR/integration"
    go build -o "$ROOT_DIR/demo" ./cmd/demo
    echo "Build complete: $ROOT_DIR/demo"
fi

if [ "$RUN" = true ]; then
    echo ""
    echo "Starting lv2k8s demo..."
    echo ""
    echo "  Web UI:    http://localhost:8000"
    echo "  Harness:   http://localhost:8080/api/status"
    echo "  Libvirt:   http://localhost:8081/api/status"
    echo "  K8s:       http://localhost:8082/api/status"
    echo "  Operator:  http://localhost:8083/api/status"
    echo ""
    echo "Press Ctrl+C to stop"
    echo ""

    cd "$ROOT_DIR"
    exec ./demo --webui-dir "$ROOT_DIR/dev/webui"
fi
