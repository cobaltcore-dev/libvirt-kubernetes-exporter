// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Operator panel controller

const operatorPanel = {
    domains: {},
    queue: { pending: 0, size: 0, totalEnqueued: 0, totalDropped: 0 },
    logs: [],
    maxLogs: 200,

    async init() {
        await this.refresh();

        // Subscribe to operator SSE events
        sse.on('operator', 'domain_created', () => this.loadDomains());
        sse.on('operator', 'domain_updated', () => this.loadDomains());
        sse.on('operator', 'domain_removed', () => this.loadDomains());
        sse.on('operator', 'domain_gone', () => this.loadDomains());
        sse.on('operator', 'queue_updated', (e) => {
            if (e.data) {
                this.queue = e.data;
                this.renderQueue();
            }
        });
    },

    async refresh() {
        await Promise.all([
            this.loadDomains(),
            this.loadQueue()
        ]);
    },

    async loadDomains() {
        try {
            const domains = await API.operator.domains();
            this.domains = {};
            domains.forEach(d => this.domains[d.uuid] = d);
            this.renderDomains();
        } catch (err) {
            console.error('Failed to load operator domains:', err);
        }
    },

    async loadQueue() {
        try {
            const queue = await API.operator.queue();
            this.queue = queue;
            this.renderQueue();
        } catch (err) {
            console.error('Failed to load queue:', err);
        }
    },

    renderDomains() {
        const container = document.getElementById('operator-domain-list');
        if (!container) return;

        const domains = Object.values(this.domains);

        if (domains.length === 0) {
            container.innerHTML = '<div class="loading">No tracked domains</div>';
            return;
        }

        // Sort by name for stable display
        const sorted = [...domains].sort((a, b) => (a.name || '').localeCompare(b.name || ''));

        container.innerHTML = sorted.map(d => `
            <div class="domain-item" data-uuid="${d.uuid}">
                <div class="domain-info">
                    <div class="domain-name">${this.escapeHtml(d.name)}</div>
                    <div class="domain-meta">
                        UUID: ${d.uuid}<br>
                        Last seen: ${this.formatTime(d.lastSeen)}
                    </div>
                </div>
                <div class="domain-actions">
                    <span class="state-badge state-${d.state}">${d.state}</span>
                </div>
            </div>
        `).join('');
    },

    renderQueue() {
        const statsContainer = document.getElementById('queue-stats');
        const itemsContainer = document.getElementById('queue-items');
        if (!statsContainer) return;

        statsContainer.innerHTML = `
            <div class="stat-box">
                <div class="stat-value">${this.queue.pending || 0}</div>
                <div class="stat-label">Pending</div>
            </div>
            <div class="stat-box">
                <div class="stat-value">${this.queue.size || 0}</div>
                <div class="stat-label">Queue Size</div>
            </div>
            <div class="stat-box">
                <div class="stat-value">${this.queue.totalEnqueued || 0}</div>
                <div class="stat-label">Total Enqueued</div>
            </div>
            <div class="stat-box">
                <div class="stat-value">${this.queue.totalDropped || 0}</div>
                <div class="stat-label">Dropped</div>
            </div>
        `;

        if (itemsContainer) {
            if (this.queue.pendingUUIDs && this.queue.pendingUUIDs.length > 0) {
                itemsContainer.innerHTML = `
                    <h3 style="margin-top: 1rem; font-size: 0.9rem; color: var(--text-secondary);">Pending UUIDs</h3>
                    <ul style="margin-top: 0.5rem; font-family: monospace; font-size: 0.8rem;">
                        ${this.queue.pendingUUIDs.map(u => `<li>${u}</li>`).join('')}
                    </ul>
                `;
            } else {
                itemsContainer.innerHTML = '';
            }
        }
    },

    renderLogs() {
        const container = document.getElementById('operator-log-list');
        if (!container) return;

        if (this.logs.length === 0) {
            container.innerHTML = '<div class="log-entry">Waiting for logs...</div>';
            return;
        }

        container.innerHTML = this.logs.map(log => `
            <div class="log-entry">
                <span class="timestamp">${this.formatTime(log.timestamp)}</span>
                <span class="message">${this.escapeHtml(log.message)}</span>
            </div>
        `).join('');

        // Auto-scroll to bottom if checkbox is checked
        const autoScroll = document.getElementById('log-autoscroll');
        if (autoScroll && autoScroll.checked) {
            container.scrollTop = container.scrollHeight;
        }
    },

    addLog(log) {
        this.logs.push(log);
        if (this.logs.length > this.maxLogs) {
            this.logs.shift();
        }
        this.renderLogs();
    },

    clearLogs() {
        this.logs = [];
        this.renderLogs();
    },

    formatTime(timestamp) {
        if (!timestamp) return 'unknown';
        const date = new Date(timestamp);
        return date.toLocaleTimeString();
    },

    escapeHtml(text) {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
};
