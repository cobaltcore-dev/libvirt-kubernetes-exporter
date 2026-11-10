// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Main application entry point

const app = {
    operatorLogSource: null,
    operatorLogRetryTimeout: null,

    async init() {
        console.log('Initializing lv2k8s UI...');

        // Initialize tab navigation first
        tabs.init();

        // Connect SSE streams for each component
        sse.connect('libvirt', `${api.libvirtUrl}/api/events`);
        sse.connect('k8s', `${api.k8sUrl}/api/events`);
        sse.connect('operator', `${api.operatorUrl}/api/events`);
        sse.connect('harness', `${api.harnessUrl}/api/events`);

        // Set up operator log stream
        this.connectOperatorLogs();

        // Initialize all panels
        try {
            await Promise.all([
                this.initPanel(overviewPanel),
                this.initPanel(libvirtPanel),
                this.initPanel(k8sPanel),
                this.initPanel(operatorPanel),
                this.initPanel(harnessPanel),
                this.initPanel(tracesPanel)
            ]);
        } catch (err) {
            console.error('Panel init error (some components may be offline):', err);
        }

        // Update connection status indicators
        this.updateConnectionStatus();
        setInterval(() => this.updateConnectionStatus(), 5000);

        console.log('UI initialized');
    },

    async initPanel(panel) {
        if (panel && typeof panel.init === 'function') {
            try {
                await panel.init();
            } catch (err) {
                console.warn(`Panel init warning:`, err.message);
            }
        }
    },

    async updateConnectionStatus() {
        const components = [
            { name: 'harness', url: api.harnessUrl, dotId: 'harness-status' },
            { name: 'libvirt', url: api.libvirtUrl, dotId: 'libvirt-status' },
            { name: 'k8s', url: api.k8sUrl, dotId: 'k8s-status' },
            { name: 'operator', url: api.operatorUrl, dotId: 'operator-status' }
        ];

        for (const comp of components) {
            try {
                const response = await fetch(`${comp.url}/api/status`, {
                    method: 'GET',
                    signal: AbortSignal.timeout(2000)
                });
                this.setConnected(comp.dotId, response.ok);
            } catch {
                this.setConnected(comp.dotId, false);
            }
        }
    },

    setConnected(dotId, connected) {
        const dot = document.getElementById(dotId);
        if (dot) {
            dot.classList.toggle('connected', connected);
        }
    },

    connectOperatorLogs() {
        // Clean up any existing connection/retry
        if (this.operatorLogSource) {
            this.operatorLogSource.close();
            this.operatorLogSource = null;
        }
        if (this.operatorLogRetryTimeout) {
            clearTimeout(this.operatorLogRetryTimeout);
            this.operatorLogRetryTimeout = null;
        }

        const url = `${api.operatorUrl}/api/logs`;
        const source = new EventSource(url);
        this.operatorLogSource = source;

        source.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                const logEntry = data.data || data;
                operatorPanel.addLog({
                    timestamp: logEntry.timestamp || data.timestamp || new Date().toISOString(),
                    message: logEntry.message || (typeof logEntry === 'string' ? logEntry : JSON.stringify(logEntry))
                });
            } catch {
                operatorPanel.addLog({
                    timestamp: new Date().toISOString(),
                    message: event.data
                });
            }
        };

        source.onerror = () => {
            if (source.readyState === EventSource.CLOSED) {
                this.operatorLogSource = null;

                if (!this.operatorLogRetryTimeout) {
                    console.warn('Operator log stream disconnected, will retry in 5s...');
                    this.operatorLogRetryTimeout = setTimeout(() => {
                        this.operatorLogRetryTimeout = null;
                        this.connectOperatorLogs();
                    }, 5000);
                }
            }
        };
    }
};

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => app.init());
