// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// fake-k8s panel controller

const k8sPanel = {
    resources: [],
    faults: [],
    requests: [],
    resourceFilter: 'all',
    filterDropdownOpen: false,

    async init() {
        await this.refresh();

        // Subscribe to events
        sse.on('k8s', 'resource_updated', () => this.loadResources());
        sse.on('k8s', 'resource_deleted', () => this.loadResources());
        sse.on('k8s', 'fault_added', () => this.loadFaults());
        sse.on('k8s', 'fault_removed', () => this.loadFaults());

        // Start heartbeat age updater
        this.startAgeUpdater();
    },

    async refresh() {
        await Promise.all([
            this.loadResources(),
            this.loadFaults(),
            this.loadRequests()
        ]);
    },

    async loadResources() {
        try {
            this.resources = await API.k8s.resources();
            this.renderResources();
        } catch (err) {
            console.error('Failed to load resources:', err);
        }
    },

    async loadFaults() {
        try {
            this.faults = await API.k8s.faults();
            this.renderFaults();
        } catch (err) {
            console.error('Failed to load faults:', err);
        }
    },

    async loadRequests() {
        try {
            this.requests = await API.k8s.requests(100);
            this.renderRequests();
        } catch (err) {
            console.error('Failed to load requests:', err);
        }
    },

    filterChanged() {
        const select = document.getElementById('k8s-resource-filter');
        if (select) {
            this.resourceFilter = select.value;
            this.renderResources();
        }
    },

    renderResources() {
        const container = document.getElementById('k8s-resource-list');
        if (!container) return;

        if (this.resources.length === 0) {
            container.innerHTML = '<div class="loading">No resources</div>';
            return;
        }

        // Update filter dropdown
        const kinds = [...new Set(this.resources.map(r => r.kind))].sort();
        const filterSelect = document.getElementById('k8s-resource-filter');
        if (filterSelect && !this.filterDropdownOpen) {
            const currentValue = filterSelect.value;
            filterSelect.innerHTML = `
                <option value="all">All (${this.resources.length})</option>
                ${kinds.map(k => {
                    const count = this.resources.filter(r => r.kind === k).length;
                    return `<option value="${k}">${k} (${count})</option>`;
                }).join('')}
            `;
            filterSelect.value = currentValue || 'all';
        }

        // Filter resources
        let filtered = this.resources;
        if (this.resourceFilter !== 'all') {
            filtered = this.resources.filter(r => r.kind === this.resourceFilter);
        }

        // Sort by kind then name
        const sorted = [...filtered].sort((a, b) => {
            if (a.kind !== b.kind) return a.kind.localeCompare(b.kind);
            return a.name.localeCompare(b.name);
        });

        // Group by kind
        const grouped = {};
        sorted.forEach(r => {
            if (!grouped[r.kind]) grouped[r.kind] = [];
            grouped[r.kind].push(r);
        });

        let html = '';
        for (const kind of Object.keys(grouped).sort()) {
            html += `<div class="resource-group">
                <div class="resource-group-header">${kind}</div>
                ${grouped[kind].map(r => this.renderResourceItem(r)).join('')}
            </div>`;
        }

        container.innerHTML = html || '<div class="loading">No matching resources</div>';
    },

    renderResourceItem(r) {
        return `
            <div class="resource-item">
                <div class="resource-info">
                    <div class="resource-name">${this.escapeHtml(r.name)}</div>
                    <div class="resource-meta">
                        ${r.namespace || 'cluster'}
                        ${r.status?.state ? ` | <span class="state-badge state-${r.status.state}">${r.status.state}</span>` : ''}
                        ${r.status?.observed ? ` | <span class="heartbeat-age" data-timestamp="${r.status.observed}">${this.formatAge(r.status.observed)}</span>` : ''}
                    </div>
                </div>
                <div class="resource-actions">
                    <button class="btn btn-small btn-danger" onclick="k8sPanel.deleteResource('${r.apiVersion}', '${r.kind}', '${r.namespace}', '${r.name}')">Delete</button>
                </div>
            </div>
        `;
    },

    renderFaults() {
        const container = document.getElementById('fault-list');
        if (!container) return;

        if (this.faults.length === 0) {
            container.innerHTML = '<div class="loading">No fault rules</div>';
            return;
        }

        container.innerHTML = this.faults.map(f => `
            <div class="fault-item">
                <div class="fault-info">
                    <code>${this.escapeHtml(f.pattern)}</code>
                    ${f.delay_ms ? ` - Delay: ${f.delay_ms}ms` : ''}
                    ${f.error_code ? ` - Error: ${f.error_code} ${this.escapeHtml(f.error_message || '')}` : ''}
                </div>
                <button class="btn btn-small btn-danger" onclick="k8sPanel.removeFault('${this.escapeHtml(f.pattern)}')">Remove</button>
            </div>
        `).join('');
    },

    renderRequests() {
        const container = document.getElementById('request-list');
        if (!container) return;

        if (this.requests.length === 0) {
            container.innerHTML = '<div class="loading">No requests</div>';
            return;
        }

        const sorted = [...this.requests].reverse();

        container.innerHTML = sorted.slice(0, 50).map(r => `
            <div class="request-item">
                <span class="method ${r.Method}">${r.Method}</span>
                <span class="path">${this.escapeHtml(r.Path)}</span>
            </div>
        `).join('');
    },

    async deleteResource(apiVersion, kind, namespace, name) {
        try {
            const [group, version] = apiVersion.includes('/')
                ? apiVersion.split('/')
                : ['', apiVersion];
            const resource = kind.toLowerCase() + 's';
            await API.k8s.deleteResource(group, version, resource, namespace, name);
            await this.loadResources();
        } catch (err) {
            alert(`Failed to delete: ${err.message}`);
        }
    },

    showFaultDialog() {
        document.getElementById('add-fault-modal').classList.remove('hidden');
        document.getElementById('add-fault-form').reset();
    },

    hideFaultDialog() {
        document.getElementById('add-fault-modal').classList.add('hidden');
    },

    async addFault(event) {
        event.preventDefault();

        const form = event.target;
        const fault = {
            pattern: form.pattern.value,
            delay_ms: parseInt(form.delay_ms.value) || 0,
            error_code: parseInt(form.error_code.value) || 0,
            error_message: form.error_message.value || ''
        };

        try {
            await API.k8s.addFault(fault);
            this.hideFaultDialog();
            await this.loadFaults();
        } catch (err) {
            alert(`Failed to add fault: ${err.message}`);
        }

        return false;
    },

    async removeFault(pattern) {
        try {
            await API.k8s.removeFault(pattern);
            await this.loadFaults();
        } catch (err) {
            alert(`Failed to remove fault: ${err.message}`);
        }
    },

    async clearRequests() {
        try {
            await API.k8s.clearRequests();
            this.requests = [];
            this.renderRequests();
        } catch (err) {
            alert(`Failed to clear: ${err.message}`);
        }
    },

    escapeHtml(text) {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    },

    formatAge(timestamp) {
        if (!timestamp) return 'unknown';
        const then = new Date(timestamp);
        const now = new Date();
        const seconds = Math.floor((now - then) / 1000);

        if (seconds < 0) return 'just now';
        if (seconds < 5) return 'just now';
        if (seconds < 60) return `${seconds}s ago`;
        if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
        return `${Math.floor(seconds / 3600)}h ago`;
    },

    startAgeUpdater() {
        setInterval(() => {
            document.querySelectorAll('.heartbeat-age').forEach(el => {
                const ts = el.dataset.timestamp;
                if (ts) {
                    el.textContent = this.formatAge(ts);
                    const age = (new Date() - new Date(ts)) / 1000;
                    el.classList.toggle('stale', age > 10);
                }
            });
        }, 1000);
    }
};
