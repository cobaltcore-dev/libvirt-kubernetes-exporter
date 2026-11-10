// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// fake-libvirt panel controller

const libvirtPanel = {
    domains: [],

    async init() {
        await this.refresh();

        // Subscribe to events
        sse.on('libvirt', 'lifecycle_event', (event) => {
            console.log('Libvirt lifecycle event:', event);
            this.refresh();
        });
    },

    async refresh() {
        try {
            this.domains = await API.libvirt.domains();
            this.render();
        } catch (err) {
            this.renderError(err.message);
        }
    },

    render() {
        const container = document.getElementById('libvirt-domains');

        if (this.domains.length === 0) {
            container.innerHTML = '<div class="loading">No domains</div>';
            return;
        }

        // Sort by name for stable display
        const sorted = [...this.domains].sort((a, b) => a.name.localeCompare(b.name));

        container.innerHTML = sorted.map(d => `
            <div class="domain-item" data-uuid="${d.uuid}">
                <div class="domain-info">
                    <div class="domain-name">${this.escapeHtml(d.name)}</div>
                    <div class="domain-meta">
                        UUID: ${d.uuid}<br>
                        vCPUs: ${d.vcpus} | Memory: ${this.formatMemory(d.memoryKB)}
                    </div>
                </div>
                <div class="domain-actions">
                    <span class="state-badge state-${d.state}">${d.state}</span>
                    ${this.renderStateButtons(d)}
                    <button class="btn btn-small btn-danger" onclick="libvirtPanel.deleteDomain('${d.uuid}')">Delete</button>
                </div>
            </div>
        `).join('');
    },

    renderStateButtons(domain) {
        const buttons = [];

        if (domain.state === 'shutoff') {
            buttons.push(`<button class="btn btn-small" onclick="libvirtPanel.setState('${domain.uuid}', 'running')">Start</button>`);
        } else if (domain.state === 'running') {
            buttons.push(`<button class="btn btn-small" onclick="libvirtPanel.setState('${domain.uuid}', 'paused')">Pause</button>`);
            buttons.push(`<button class="btn btn-small" onclick="libvirtPanel.setState('${domain.uuid}', 'shutoff')">Stop</button>`);
        } else if (domain.state === 'paused') {
            buttons.push(`<button class="btn btn-small" onclick="libvirtPanel.setState('${domain.uuid}', 'running')">Resume</button>`);
            buttons.push(`<button class="btn btn-small" onclick="libvirtPanel.setState('${domain.uuid}', 'shutoff')">Stop</button>`);
        }

        return buttons.join('');
    },

    renderError(message) {
        const container = document.getElementById('libvirt-domains');
        container.innerHTML = `<div class="loading" style="color: var(--error)">Error: ${this.escapeHtml(message)}</div>`;
    },

    async setState(uuid, state) {
        try {
            await API.libvirt.setDomainState(uuid, state);
            await this.refresh();
        } catch (err) {
            alert(`Failed to set state: ${err.message}`);
        }
    },

    async deleteDomain(uuid) {
        try {
            await API.libvirt.deleteDomain(uuid);
            await this.refresh();
        } catch (err) {
            alert(`Failed to delete: ${err.message}`);
        }
    },

    showAddDialog() {
        document.getElementById('add-domain-modal').classList.remove('hidden');
        document.getElementById('add-domain-form').reset();
    },

    hideAddDialog() {
        document.getElementById('add-domain-modal').classList.add('hidden');
    },

    async addDomain(event) {
        event.preventDefault();

        const form = event.target;
        const domain = {
            name: form.name.value,
            state: form.state.value,
            maxMemKB: parseInt(form.maxMemKB.value),
            memoryKB: parseInt(form.memoryKB.value),
            vcpus: parseInt(form.vcpus.value)
        };

        try {
            await API.libvirt.createDomain(domain);
            this.hideAddDialog();
            await this.refresh();
        } catch (err) {
            alert(`Failed to add domain: ${err.message}`);
        }

        return false;
    },

    formatMemory(kb) {
        if (kb >= 1048576) {
            return `${(kb / 1048576).toFixed(1)} GB`;
        }
        return `${(kb / 1024).toFixed(0)} MB`;
    },

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
};
