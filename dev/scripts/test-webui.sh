#!/bin/bash


# Test the WebUI with headless Chrome
# Requires the demo to be running on localhost:8000

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
SCREENSHOT_DIR="$ROOT_DIR/webui-screenshots"

CHROME="${CHROME:-google-chrome}"
BASE_URL="${BASE_URL:-http://localhost:8000}"

# Check if Chrome is available
if ! command -v "$CHROME" &> /dev/null; then
    echo "Error: Chrome not found. Set CHROME env var to chrome binary path."
    exit 1
fi

# Check if demo is running
if ! curl -s "$BASE_URL" > /dev/null 2>&1; then
    echo "Error: Demo not running at $BASE_URL"
    echo "Start it with: ./dev/scripts/run-demo.sh"
    exit 1
fi

mkdir -p "$SCREENSHOT_DIR"

echo "Testing WebUI with headless Chrome..."
echo "Screenshots will be saved to: $SCREENSHOT_DIR"
echo ""

# Function to take screenshot
take_screenshot() {
    local url="$1"
    local name="$2"

    echo "  Capturing $name..."

    timeout 15 "$CHROME" \
        --headless=new \
        --disable-gpu \
        --no-sandbox \
        --disable-dev-shm-usage \
        --window-size=1280,800 \
        --screenshot="$SCREENSHOT_DIR/$name.png" \
        "$url" 2>/dev/null || true

    if [ -f "$SCREENSHOT_DIR/$name.png" ]; then
        local size=$(stat -c%s "$SCREENSHOT_DIR/$name.png" 2>/dev/null || echo "0")
        if [ "$size" -gt 1000 ]; then
            echo "    OK: $SCREENSHOT_DIR/$name.png ($size bytes)"
            return 0
        fi
    fi
    echo "    Warning: Screenshot may not have rendered correctly"
    return 1
}

echo "1. Taking screenshots of each tab..."
echo ""

# Main page (Overview tab)
take_screenshot "$BASE_URL" "01-overview"

# We can't easily switch tabs via URL hash with headless Chrome rendering
# but we can verify the page structure loads

# Standalone trace viewer
take_screenshot "$BASE_URL/traces.html" "02-traces-standalone"

echo ""
echo "2. Testing HTTP endpoints..."
echo ""

# Test main page
echo "  Checking $BASE_URL..."
RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL")
if [ "$RESPONSE" = "200" ]; then
    echo "    PASS: Main page returns 200"
else
    echo "    FAIL: Main page returns $RESPONSE"
fi

# Test static files
for file in "js/api.js" "js/tabs.js" "js/timeline.js" "js/app.js" "css/style.css" "traces.html"; do
    RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/$file")
    if [ "$RESPONSE" = "200" ]; then
        echo "    PASS: $file loads (200)"
    else
        echo "    FAIL: $file returns $RESPONSE"
    fi
done

echo ""
echo "3. Testing API endpoints..."
echo ""

# Test API endpoints on each port
test_api() {
    local port=$1
    local name=$2
    local endpoint=$3

    RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:$port$endpoint" 2>/dev/null || echo "000")
    if [ "$RESPONSE" = "200" ]; then
        echo "    PASS: $name $endpoint (200)"
    else
        echo "    WARN: $name $endpoint returns $RESPONSE"
    fi
}

test_api 8081 "Libvirt" "/api/status"
test_api 8081 "Libvirt" "/api/domains"
test_api 8082 "K8s" "/api/status"
test_api 8082 "K8s" "/api/resources"
test_api 8083 "Operator" "/api/status"
test_api 8083 "Operator" "/api/domains"

echo ""
echo "4. Verifying page content..."
echo ""

# Check that the HTML contains expected elements
CONTENT=$(curl -s "$BASE_URL")

check_content() {
    local pattern="$1"
    local description="$2"
    if echo "$CONTENT" | grep -q "$pattern"; then
        echo "    PASS: $description"
    else
        echo "    FAIL: $description not found"
    fi
}

check_content 'class="main-tab"' "Tab navigation present"
check_content 'data-tab="overview"' "Overview tab button"
check_content 'data-tab="traces"' "Traces tab button"
check_content 'id="trace-canvas"' "Trace canvas element"
check_content 'architecture-diagram' "Architecture diagram"
check_content 'timeline.js' "Timeline JS reference"

echo ""
echo "Done!"
echo ""
echo "Screenshots:"
ls -la "$SCREENSHOT_DIR"/*.png 2>/dev/null || echo "(none)"
