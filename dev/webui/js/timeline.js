// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Canvas-based timeline renderer for trace visualization
class Timeline {
    constructor(canvas) {
        this.canvas = canvas;
        this.ctx = canvas.getContext('2d');
        this.events = [];
        this.tracks = new Map(); // trackId -> track info

        // View state
        this.scale = 1.0; // pixels per millisecond
        this.offset = 0;  // scroll offset in milliseconds
        this.autoScroll = true;

        // Layout
        this.trackHeight = 30;
        this.trackPadding = 5;
        this.labelWidth = 120;
        this.headerHeight = 30;

        // Interaction state
        this.hoveredEvent = null;
        this.selectedEvent = null;
        this.cursor = null; // cursor time in ms

        // Colors
        this.colors = {
            observer: '#4CAF50',
            tracker: '#2196F3',
            exporter: '#FF9800',
            lifecycle: '#9C27B0',
            scenario: '#F44336',
            background: '#1e1e1e',
            grid: '#333',
            text: '#e0e0e0',
            cursor: '#ff0'
        };

        this.setupEventHandlers();
        this.resize();
    }

    setupEventHandlers() {
        // Zoom with mouse wheel
        this.canvas.addEventListener('wheel', e => {
            e.preventDefault();
            const rect = this.canvas.getBoundingClientRect();
            const x = e.clientX - rect.left;
            const centerTime = this.screenToTime(x);

            const factor = e.deltaY > 0 ? 0.9 : 1.1;
            this.scale *= factor;
            this.scale = Math.max(0.01, Math.min(100, this.scale));

            // Keep center time at same position
            this.offset = centerTime - (x - this.labelWidth) / this.scale;

            this.autoScroll = false;
            this.render();
        });

        // Pan with mouse drag
        let dragging = false;
        let lastX = 0;

        this.canvas.addEventListener('mousedown', e => {
            dragging = true;
            lastX = e.clientX;
            this.canvas.style.cursor = 'grabbing';
        });

        this.canvas.addEventListener('mousemove', e => {
            if (dragging) {
                const dx = e.clientX - lastX;
                this.offset -= dx / this.scale;
                lastX = e.clientX;
                this.autoScroll = false;
                this.render();
            } else {
                // Hover detection
                this.handleHover(e);
            }
        });

        this.canvas.addEventListener('mouseup', () => {
            dragging = false;
            this.canvas.style.cursor = 'default';
        });

        this.canvas.addEventListener('mouseleave', () => {
            dragging = false;
            this.hoveredEvent = null;
            this.render();
        });

        // Click to place cursor
        this.canvas.addEventListener('click', e => {
            const rect = this.canvas.getBoundingClientRect();
            const x = e.clientX - rect.left;
            if (x > this.labelWidth) {
                this.cursor = this.screenToTime(x);
                this.render();
            }
        });

        // Resize observer
        const resizeObserver = new ResizeObserver(() => this.resize());
        resizeObserver.observe(this.canvas.parentElement);
    }

    resize() {
        const parent = this.canvas.parentElement;
        this.canvas.width = parent.clientWidth;
        this.canvas.height = parent.clientHeight;
        this.render();
    }

    loadTrace(traceData) {
        this.events = traceData.traceEvents || [];
        this.buildTracks();
        this.fitToContent();
        this.render();
    }

    addEvents(newEvents) {
        this.events.push(...newEvents);
        this.buildTracks();
        if (this.autoScroll) {
            this.scrollToEnd();
        }
        this.render();
    }

    buildTracks() {
        this.tracks.clear();

        // Group events by category
        for (const event of this.events) {
            const cat = event.cat || 'default';
            if (!this.tracks.has(cat)) {
                this.tracks.set(cat, {
                    name: cat,
                    events: [],
                    color: this.colors[cat] || '#666'
                });
            }
            this.tracks.get(cat).events.push(event);
        }
    }

    fitToContent() {
        if (this.events.length === 0) return;

        // Timestamps are in microseconds, convert to ms
        const minTime = Math.min(...this.events.map(e => e.ts / 1000));
        const maxTime = Math.max(...this.events.map(e => (e.ts + (e.dur || 0)) / 1000));
        const duration = maxTime - minTime; // ms

        if (duration > 0) {
            const viewWidth = this.canvas.width - this.labelWidth - 50;
            this.scale = Math.max(0.001, Math.min(100, viewWidth / duration));
            this.offset = minTime;
        } else {
            // All events at same time, use sensible defaults
            this.scale = 1;
            this.offset = minTime - 100;
        }
    }

    scrollToEnd() {
        if (this.events.length === 0) return;
        // Timestamps are in microseconds, convert to ms
        const maxTime = Math.max(...this.events.map(e => (e.ts + (e.dur || 0)) / 1000));
        const viewWidth = (this.canvas.width - this.labelWidth) / this.scale;
        this.offset = maxTime - viewWidth + 50 / this.scale;
    }

    screenToTime(x) {
        return (x - this.labelWidth) / this.scale + this.offset;
    }

    timeToScreen(t) {
        return (t - this.offset) * this.scale + this.labelWidth;
    }

    handleHover(e) {
        const rect = this.canvas.getBoundingClientRect();
        const x = e.clientX - rect.left;
        const y = e.clientY - rect.top;

        const time = this.screenToTime(x);

        let trackIndex = 0;
        for (const [_, track] of this.tracks) {
            const trackY = this.headerHeight + trackIndex * (this.trackHeight + this.trackPadding);

            if (y >= trackY && y < trackY + this.trackHeight) {
                // Find event at this time
                for (const event of track.events) {
                    // Timestamps are in microseconds, convert to ms
                    const eventTime = event.ts / 1000;
                    const eventEnd = eventTime + (event.dur || 0) / 1000;
                    const tolerance = 5 / Math.max(0.001, this.scale);

                    if (time >= eventTime - tolerance && time <= eventEnd + tolerance) {
                        if (this.hoveredEvent !== event) {
                            this.hoveredEvent = event;
                            this.render();
                        }
                        return;
                    }
                }
            }
            trackIndex++;
        }

        if (this.hoveredEvent) {
            this.hoveredEvent = null;
            this.render();
        }
    }

    render() {
        const ctx = this.ctx;
        const width = this.canvas.width;
        const height = this.canvas.height;

        // Clear
        ctx.fillStyle = this.colors.background;
        ctx.fillRect(0, 0, width, height);

        // Draw time axis
        this.drawTimeAxis();

        // Draw tracks
        let trackIndex = 0;
        for (const [name, track] of this.tracks) {
            const y = this.headerHeight + trackIndex * (this.trackHeight + this.trackPadding);
            this.drawTrack(track, y, name);
            trackIndex++;
        }

        // Draw cursor
        if (this.cursor !== null) {
            const x = this.timeToScreen(this.cursor);
            if (x >= this.labelWidth && x <= width) {
                ctx.strokeStyle = this.colors.cursor;
                ctx.setLineDash([5, 5]);
                ctx.beginPath();
                ctx.moveTo(x, 0);
                ctx.lineTo(x, height);
                ctx.stroke();
                ctx.setLineDash([]);
            }
        }

        // Draw tooltip
        if (this.hoveredEvent) {
            this.drawTooltip();
        }
    }

    drawTimeAxis() {
        const ctx = this.ctx;
        const width = this.canvas.width;

        ctx.fillStyle = '#2a2a2a';
        ctx.fillRect(0, 0, width, this.headerHeight);

        // Calculate grid spacing - prevent infinite loop with scale bounds
        let gridSpacing = 1; // ms
        const effectiveScale = Math.max(0.001, this.scale);
        let iterations = 0;
        while (gridSpacing * effectiveScale < 50 && iterations < 20) {
            gridSpacing *= 10;
            iterations++;
        }

        const startTime = Math.floor(this.offset / gridSpacing) * gridSpacing;
        const endTime = this.offset + (width - this.labelWidth) / effectiveScale;

        ctx.strokeStyle = this.colors.grid;
        ctx.fillStyle = this.colors.text;
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';

        for (let t = startTime; t <= endTime; t += gridSpacing) {
            const x = this.timeToScreen(t);
            if (x < this.labelWidth) continue;

            ctx.beginPath();
            ctx.moveTo(x, this.headerHeight - 5);
            ctx.lineTo(x, this.canvas.height);
            ctx.stroke();

            const label = this.formatTime(t);
            ctx.fillText(label, x, this.headerHeight - 10);
        }
    }

    formatTime(ms) {
        const abs = Math.abs(ms);
        const sign = ms < 0 ? '-' : '';
        if (abs < 1000) return `${sign}${abs.toFixed(0)}ms`;
        if (abs < 60000) return `${sign}${(abs/1000).toFixed(1)}s`;
        return `${sign}${(abs/60000).toFixed(1)}m`;
    }

    drawTrack(track, y, name) {
        const ctx = this.ctx;

        // Track label
        ctx.fillStyle = this.colors.text;
        ctx.font = '12px sans-serif';
        ctx.textAlign = 'right';
        ctx.fillText(name, this.labelWidth - 10, y + this.trackHeight/2 + 4);

        // Track background
        ctx.fillStyle = '#252525';
        ctx.fillRect(this.labelWidth, y, this.canvas.width - this.labelWidth, this.trackHeight);

        // Draw events
        for (const event of track.events) {
            this.drawEvent(event, track.color, y);
        }
    }

    drawEvent(event, color, trackY) {
        const ctx = this.ctx;
        // Timestamps are in microseconds, convert to ms
        const eventTimeMs = event.ts / 1000;
        const x = this.timeToScreen(eventTimeMs);
        const isHovered = event === this.hoveredEvent;

        if (event.ph === 'X' || event.ph === 'B') {
            // Span event - draw bar
            // Duration is also in microseconds
            const durationMs = (event.dur || 1000) / 1000;
            const width = Math.max(2, durationMs * this.scale);

            ctx.fillStyle = isHovered ? this.lightenColor(color) : color;
            ctx.fillRect(x, trackY + 5, width, this.trackHeight - 10);

            // Event name
            if (width > 30) {
                ctx.fillStyle = '#fff';
                ctx.font = '10px sans-serif';
                ctx.textAlign = 'left';
                ctx.fillText(event.name, x + 3, trackY + this.trackHeight/2 + 3);
            }
        } else {
            // Instant event - draw marker
            ctx.fillStyle = isHovered ? this.lightenColor(color) : color;
            ctx.beginPath();
            ctx.arc(x, trackY + this.trackHeight/2, isHovered ? 6 : 4, 0, Math.PI * 2);
            ctx.fill();
        }
    }

    lightenColor(color) {
        // Simple color lightening
        return color.replace(/[0-9a-f]{2}/gi, match => {
            const val = Math.min(255, parseInt(match, 16) + 50);
            return val.toString(16).padStart(2, '0');
        });
    }

    drawTooltip() {
        const event = this.hoveredEvent;
        const ctx = this.ctx;

        // Timestamps are in microseconds, convert to ms for display
        const eventTimeMs = event.ts / 1000;
        const x = this.timeToScreen(eventTimeMs);
        const y = 100;

        const text = [
            event.name,
            `Time: ${this.formatTime(eventTimeMs)}`,
            event.dur ? `Duration: ${this.formatTime(event.dur / 1000)}` : null,
            ...Object.entries(event.args || {}).map(([k, v]) => `${k}: ${v}`)
        ].filter(Boolean);

        const padding = 8;
        const lineHeight = 14;
        const width = Math.max(...text.map(t => ctx.measureText(t).width)) + padding * 2;
        const height = text.length * lineHeight + padding * 2;

        // Background
        ctx.fillStyle = 'rgba(0, 0, 0, 0.85)';
        ctx.fillRect(x + 10, y, width, height);

        // Text
        ctx.fillStyle = '#fff';
        ctx.font = '12px monospace';
        ctx.textAlign = 'left';
        text.forEach((line, i) => {
            ctx.fillText(line, x + 10 + padding, y + padding + (i + 1) * lineHeight - 2);
        });
    }

    setAutoScroll(enabled) {
        this.autoScroll = enabled;
    }
}

// Export for use in other modules
window.Timeline = Timeline;
