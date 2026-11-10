// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Traces panel - timeline viewer for trace files
const tracesPanel = {
    timeline: null,
    loaded: false,
    files: [],

    init() {
        const canvas = document.getElementById('trace-canvas');
        if (canvas) {
            this.timeline = new Timeline(canvas);
        }

        // Setup drag-and-drop
        const dropZone = document.getElementById('trace-drop-zone');
        if (dropZone) {
            dropZone.addEventListener('dragover', e => {
                e.preventDefault();
                dropZone.classList.add('drag-over');
            });

            dropZone.addEventListener('dragleave', () => {
                dropZone.classList.remove('drag-over');
            });

            dropZone.addEventListener('drop', e => {
                e.preventDefault();
                dropZone.classList.remove('drag-over');
                this.handleFileDrop(e.dataTransfer.files);
            });
        }

        // Prevent default drag on body
        document.body.addEventListener('dragover', e => e.preventDefault());
        document.body.addEventListener('drop', e => e.preventDefault());

        // Auto-scroll checkbox
        const autoScrollCb = document.getElementById('trace-auto-scroll');
        if (autoScrollCb) {
            autoScrollCb.addEventListener('change', () => {
                if (this.timeline) {
                    this.timeline.setAutoScroll(autoScrollCb.checked);
                }
            });
        }

        // File input
        const fileInput = document.getElementById('trace-file-input');
        if (fileInput) {
            fileInput.addEventListener('change', () => {
                this.handleFileDrop(fileInput.files);
                fileInput.value = ''; // Reset so same file can be loaded again
            });
        }

        // Handle tab changes to resize canvas
        window.addEventListener('tabchange', (e) => {
            if (e.detail.tab === 'traces' && this.timeline) {
                setTimeout(() => this.timeline.resize(), 0);
            }
        });
    },

    handleFileDrop(files) {
        for (const file of files) {
            if (file.name.endsWith('.json')) {
                this.loadFile(file);
            }
        }
    },

    loadFile(file) {
        const reader = new FileReader();
        reader.onload = e => {
            try {
                const data = JSON.parse(e.target.result);
                this.loadTrace(data, file.name);
            } catch (err) {
                console.error('Failed to parse trace file:', err);
                alert('Failed to parse trace file: ' + err.message);
            }
        };
        reader.readAsText(file);
    },

    loadTrace(data, name) {
        if (!this.timeline) {
            console.error('Timeline not initialized');
            return;
        }

        // Hide drop zone, show timeline
        const dropZone = document.getElementById('trace-drop-zone');
        const timelineContainer = document.getElementById('trace-timeline');
        if (dropZone) dropZone.classList.add('hidden');
        if (timelineContainer) timelineContainer.classList.remove('hidden');

        if (this.files.length === 0) {
            this.timeline.loadTrace(data);
        } else {
            this.timeline.addEvents(data.traceEvents || []);
        }

        this.files.push(name);
        this.loaded = true;
        this.updateInfo();

        // Trigger resize after showing
        setTimeout(() => this.timeline.resize(), 0);
    },

    async loadFromAPI(traceId) {
        try {
            // If specific trace ID provided, load that one
            const endpoint = traceId
                ? `${api.operatorUrl}/api/traces/${traceId}`
                : `${api.operatorUrl}/api/traces/latest`;

            const response = await fetch(endpoint);
            if (!response.ok) {
                if (response.status === 404) {
                    alert('No traces available. Run a test to generate trace data.');
                    return;
                }
                throw new Error(`HTTP ${response.status}`);
            }

            const data = await response.json();
            if (!data || !data.traceEvents || data.traceEvents.length === 0) {
                alert('Trace has no events');
                return;
            }

            this.loadTrace(data, data.name || `Trace ${data.id || 'latest'}`);

        } catch (err) {
            console.error('Failed to load trace from API:', err);
            alert('Failed to load trace from server: ' + err.message);
        }
    },

    async listTraces() {
        try {
            const response = await fetch(`${api.operatorUrl}/api/traces`);
            if (!response.ok) return [];
            return await response.json();
        } catch {
            return [];
        }
    },

    updateInfo() {
        const info = document.getElementById('trace-info');
        if (info && this.timeline) {
            const eventCount = this.timeline.events.length;
            info.textContent = `${this.files.length} file(s), ${eventCount} events`;
        }
    },

    clear() {
        if (this.timeline) {
            this.timeline.events = [];
            this.timeline.tracks.clear();
            this.timeline.render();
        }

        this.files = [];
        this.loaded = false;

        const dropZone = document.getElementById('trace-drop-zone');
        const timelineContainer = document.getElementById('trace-timeline');
        if (dropZone) dropZone.classList.remove('hidden');
        if (timelineContainer) timelineContainer.classList.add('hidden');

        const info = document.getElementById('trace-info');
        if (info) info.textContent = '';
    }
};
