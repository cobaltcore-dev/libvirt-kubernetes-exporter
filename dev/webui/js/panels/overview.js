// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Overview panel - architecture diagram and status summary
const overviewPanel = {
    activityLog: [],
    maxActivity: 10,

    init() {
        this.refresh();

        // Subscribe to SSE events for activity log
        window.addEventListener('sse-event', (e) => {
            this.handleEvent(e.detail);
        });

        // Periodic refresh
        setInterval(() => this.refresh(), 5000);
    },

    async refresh() {
        try {
            // Fetch status from all components
            const [libvirtStatus, operatorStatus, k8sStatus] = await Promise.all([
                this.fetchStatus(api.libvirtUrl),
                this.fetchStatus(api.operatorUrl),
                this.fetchStatus(api.k8sUrl)
            ]);

            // Update domain count from libvirt
            // API returns: {"domain_count":3, ...}
            if (libvirtStatus && libvirtStatus.domain_count !== undefined) {
                document.getElementById('overview-domain-count').textContent = libvirtStatus.domain_count;
            }

            // Update tracked count from operator
            // API returns: {"tracker":{"domain_count":3, ...}, ...}
            if (operatorStatus && operatorStatus.tracker?.domain_count !== undefined) {
                document.getElementById('overview-tracked-count').textContent = operatorStatus.tracker.domain_count;
            }

            // Update resource count from k8s
            // API returns: {"resource_count":6, ...}
            if (k8sStatus && k8sStatus.resource_count !== undefined) {
                document.getElementById('overview-resource-count').textContent = k8sStatus.resource_count;
            }

            // Update queue stats from operator
            // API returns: {"queue":{"pending_count":0, "total_dropped":0, ...}, ...}
            if (operatorStatus && operatorStatus.queue) {
                document.getElementById('overview-queue-pending').textContent = operatorStatus.queue.pending_count || 0;
                document.getElementById('overview-queue-dropped').textContent = operatorStatus.queue.total_dropped || 0;
            }

            // Update status dots
            this.updateStatusDot('overview-libvirt-status', libvirtStatus !== null);
            this.updateStatusDot('overview-operator-status', operatorStatus !== null);
            this.updateStatusDot('overview-k8s-status', k8sStatus !== null);

        } catch (err) {
            console.error('Overview refresh error:', err);
        }
    },

    async fetchStatus(baseUrl) {
        try {
            const response = await fetch(`${baseUrl}/api/status`);
            if (!response.ok) return null;
            return await response.json();
        } catch {
            return null;
        }
    },

    updateStatusDot(elementId, connected) {
        const dot = document.getElementById(elementId);
        if (dot) {
            dot.classList.toggle('connected', connected);
        }
    },

    handleEvent(event) {
        if (!event || !event.type) return;

        // Add to activity log
        const activity = this.formatActivity(event);
        if (activity) {
            this.activityLog.unshift(activity);
            if (this.activityLog.length > this.maxActivity) {
                this.activityLog.pop();
            }
            this.renderActivity();
        }

        // Update counts based on event type
        if (event.type === 'domain_created' || event.type === 'domain_deleted') {
            this.refresh();
        }
    },

    formatActivity(event) {
        const time = new Date().toLocaleTimeString();

        switch (event.type) {
            case 'domain_created':
                return `${time} Domain created: ${event.data?.name || 'unknown'}`;
            case 'domain_deleted':
                return `${time} Domain deleted: ${event.data?.name || 'unknown'}`;
            case 'domain_state_changed':
                return `${time} ${event.data?.name || 'unknown'} → ${event.data?.state || '?'}`;
            case 'resource_updated':
                return `${time} CRD updated: ${event.data?.name || 'unknown'}`;
            case 'test_started':
                return `${time} Test started: ${event.data?.name || 'unknown'}`;
            case 'test_completed':
                return `${time} Test completed: ${event.data?.result || 'unknown'}`;
            default:
                return null;
        }
    },

    renderActivity() {
        const container = document.getElementById('overview-activity');
        if (!container) return;

        if (this.activityLog.length === 0) {
            container.innerHTML = '<div class="activity-item">No recent activity</div>';
            return;
        }

        container.innerHTML = this.activityLog
            .map(a => `<div class="activity-item">${a}</div>`)
            .join('');
    }
};
