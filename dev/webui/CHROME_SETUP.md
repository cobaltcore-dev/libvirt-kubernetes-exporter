<!--
SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors

SPDX-License-Identifier: Apache-2.0
-->

# Chrome Headless Setup for Claude Code

This document describes how to set up headless Google Chrome on a headless Linux system for use with Claude Code's browser debugging capabilities.

## Prerequisites

- Ubuntu 24.04+ (or similar Debian-based distro)
- sudo access for package installation

## Installation

### Step 1: Install Chrome Dependencies

Chrome requires various libraries for rendering, fonts, and GPU abstraction:

```bash
# For Ubuntu 24.04+ (uses libasound2t64 instead of libasound2)
sudo apt-get install -y \
    libnss3 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdrm2 \
    libxkbcommon0 \
    libxcomposite1 \
    libxdamage1 \
    libxrandr2 \
    libgbm1 \
    libasound2t64

# For Ubuntu 22.04 and earlier, use libasound2 instead:
# sudo apt-get install -y libasound2
```

### Step 2: Install Google Chrome

```bash
wget -q -O /tmp/chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
sudo dpkg -i /tmp/chrome.deb
sudo apt-get install -f -y  # Fix any missing dependencies
rm /tmp/chrome.deb
```

### Step 3: Verify Installation

```bash
google-chrome --version
# Expected: Google Chrome 149.x.xxxx.xxx (or newer)
```

## Usage

### Basic Headless Commands

```bash
# Screenshot a URL
google-chrome --headless --disable-gpu --no-sandbox \
    --screenshot=/tmp/screenshot.png \
    --window-size=1280,800 \
    "http://localhost:8000"

# Dump DOM as HTML
google-chrome --headless --disable-gpu --no-sandbox \
    --dump-dom \
    "http://localhost:8000"

# Generate PDF
google-chrome --headless --disable-gpu --no-sandbox \
    --print-to-pdf=/tmp/output.pdf \
    "http://localhost:8000"
```

### Common Flags

| Flag | Purpose |
|------|---------|
| `--headless` | Run without a display |
| `--disable-gpu` | Disable GPU acceleration (required on most headless servers) |
| `--no-sandbox` | Disable sandbox (required when running as root or in containers) |
| `--window-size=W,H` | Set viewport dimensions |
| `--screenshot=PATH` | Save screenshot to file |
| `--dump-dom` | Output rendered DOM to stdout |
| `--print-to-pdf=PATH` | Save page as PDF |
| `--virtual-time-budget=MS` | Wait for JS to execute (milliseconds) |

## Using with Claude Code

### Enable Browser Support

Start Claude Code with the `--chrome` flag:

```bash
claude --chrome
```

This enables:
- `/verify` skill - Screenshot and interact with pages to verify changes
- `/run` skill - Launch and observe the running application

### Example Workflow

1. Start the demo server:
   ```bash
   ./scripts/run-demo.sh
   ```

2. In Claude Code, use `/verify` or `/run` to screenshot `http://localhost:8000`

3. Claude can see the rendered UI and help debug visual issues

## Troubleshooting

### "cannot open shared object file"

Missing dependency. Run:
```bash
sudo apt-get install -f -y
```

### Screenshot is blank

- Ensure you're using `file://` or `http://` URLs, not `data:` URIs
- Add `--virtual-time-budget=5000` for pages with JavaScript

### "Running as root without --no-sandbox is not supported"

Add `--no-sandbox` flag (acceptable for local development/testing).

### Font rendering issues

Install additional fonts:
```bash
sudo apt-get install -y fonts-liberation fonts-noto-core
```

## Installed Packages Reference

The full installation on Ubuntu 24.04 includes these packages (auto-installed as dependencies):

- **Core rendering**: libcairo2, libpango-1.0-0, libgtk-3-0t64
- **Graphics**: libgbm1, mesa-libgallium, libvulkan1, libegl1, libgl1
- **X11 compatibility**: libx11-xcb1, libxcomposite1, libxdamage1, libxfixes3, libxrandr2
- **Fonts**: fonts-liberation, fonts-liberation-sans-narrow
- **Accessibility**: at-spi2-core, libatk1.0-0t64, libatk-bridge2.0-0t64
- **Audio**: libasound2t64
- **Misc**: libcups2t64, libnss3

## Claude Code Capabilities

With headless Chrome, Claude Code can:

| Capability | How | Limitation |
|------------|-----|------------|
| View rendered UI | Screenshot PNG at any resolution | Static snapshot only |
| Inspect layout | Set viewport with `--window-size` | No interaction/clicking |
| Compare changes | Before/after screenshots | Must wait for async content |
| Get HTML structure | `--dump-dom` | Captures pre-JavaScript state |
| Export pages | `--print-to-pdf` | Full page, not viewport |

**Typical workflow:**
1. Start server (e.g., `./scripts/run-demo.sh`)
2. Screenshot at 1280x900 (good default for 4-panel layouts)
3. Claude sees the PNG and analyzes layout, colors, state badges, text
4. Identify issues or confirm fixes visually

**Not possible:** Clicking buttons, filling forms, reading console logs, inspecting network requests. For those, use browser DevTools manually or Puppeteer/Playwright scripts.

## Quick Test

Run this to verify everything works:

```bash
echo '<h1>Test</h1>' > /tmp/test.html
google-chrome --headless --disable-gpu --no-sandbox \
    --screenshot=/tmp/test.png \
    --window-size=800,600 \
    "file:///tmp/test.html"
file /tmp/test.png  # Should show: PNG image data, 800 x 600
```
