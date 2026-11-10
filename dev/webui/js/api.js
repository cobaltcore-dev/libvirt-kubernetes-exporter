// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// API client for all components

// Convenience URLs for direct use
const api = {
    harnessUrl: 'http://localhost:8080',
    libvirtUrl: 'http://localhost:8081',
    k8sUrl: 'http://localhost:8082',
    operatorUrl: 'http://localhost:8083'
};

const API = {
    // Default ports - can be overridden
    ports: {
        harness: 8080,
        libvirt: 8081,
        k8s: 8082,
        operator: 8083
    },

    // Get base URL for a component
    baseURL(component) {
        const port = this.ports[component];
        return `http://localhost:${port}`;
    },

    // Generic fetch with error handling
    async fetch(component, path, options = {}) {
        const url = `${this.baseURL(component)}${path}`;
        try {
            const response = await fetch(url, {
                ...options,
                headers: {
                    'Content-Type': 'application/json',
                    ...options.headers
                }
            });

            if (!response.ok) {
                const error = await response.json().catch(() => ({ error: response.statusText }));
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            if (response.status === 204) {
                return null;
            }

            return await response.json();
        } catch (err) {
            console.error(`API error (${component}${path}):`, err);
            throw err;
        }
    },

    // Harness API
    harness: {
        async status() {
            return API.fetch('harness', '/api/status');
        },
        async components() {
            return API.fetch('harness', '/api/components');
        },
        async tests() {
            return API.fetch('harness', '/api/tests');
        },
        async runTest(name) {
            return API.fetch('harness', '/api/tests', {
                method: 'POST',
                body: JSON.stringify({ name })
            });
        },
        async getTest(name) {
            return API.fetch('harness', `/api/tests/${name}`);
        },
        async currentTest() {
            return API.fetch('harness', '/api/tests/current');
        },
        async stopTest() {
            return API.fetch('harness', '/api/tests/current', { method: 'DELETE' });
        }
    },

    // fake-libvirt API
    libvirt: {
        async status() {
            return API.fetch('libvirt', '/api/status');
        },
        async domains() {
            return API.fetch('libvirt', '/api/domains');
        },
        async getDomain(uuid) {
            return API.fetch('libvirt', `/api/domains/${uuid}`);
        },
        async createDomain(domain) {
            return API.fetch('libvirt', '/api/domains', {
                method: 'POST',
                body: JSON.stringify(domain)
            });
        },
        async deleteDomain(uuid) {
            return API.fetch('libvirt', `/api/domains/${uuid}`, { method: 'DELETE' });
        },
        async setDomainState(uuid, state) {
            return API.fetch('libvirt', `/api/domains/${uuid}/state`, {
                method: 'PUT',
                body: JSON.stringify({ state })
            });
        }
    },

    // fake-k8s API
    k8s: {
        async status() {
            return API.fetch('k8s', '/api/status');
        },
        async resources(filters = {}) {
            const params = new URLSearchParams(filters);
            const query = params.toString() ? `?${params}` : '';
            return API.fetch('k8s', `/api/resources${query}`);
        },
        async deleteResource(group, version, resource, namespace, name) {
            return API.fetch('k8s', `/api/resources/${group}/${version}/${resource}/${namespace}/${name}`, {
                method: 'DELETE'
            });
        },
        async faults() {
            return API.fetch('k8s', '/api/faults');
        },
        async addFault(fault) {
            return API.fetch('k8s', '/api/faults', {
                method: 'POST',
                body: JSON.stringify(fault)
            });
        },
        async removeFault(pattern) {
            return API.fetch('k8s', `/api/faults/${encodeURIComponent(pattern)}`, {
                method: 'DELETE'
            });
        },
        async requests(limit = 50) {
            return API.fetch('k8s', `/api/requests?limit=${limit}`);
        },
        async clearRequests() {
            return API.fetch('k8s', '/api/requests', { method: 'DELETE' });
        },
        async reset() {
            return API.fetch('k8s', '/api/reset', { method: 'POST' });
        }
    },

    // Operator API
    operator: {
        async status() {
            return API.fetch('operator', '/api/status');
        },
        async config() {
            return API.fetch('operator', '/api/config');
        },
        async domains() {
            return API.fetch('operator', '/api/domains');
        },
        async getDomain(uuid) {
            return API.fetch('operator', `/api/domains/${uuid}`);
        },
        async queue() {
            return API.fetch('operator', '/api/queue');
        }
    }
};
