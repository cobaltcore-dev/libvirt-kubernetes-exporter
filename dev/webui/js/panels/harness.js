// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Harness panel controller

const harnessPanel = {
    tests: [],
    currentTest: null,
    results: [],
    isRefreshing: false,
    queue: [],
    queuePaused: false,
    selectedTest: null,
    selectedResult: null,

    async init() {
        await this.refresh();

        // Subscribe to events
        sse.on('harness', 'test_started', (e) => {
            this.currentTest = e.data;
            this.render();
        });

        sse.on('harness', 'test_progress', (e) => {
            if (this.currentTest && e.data) {
                this.currentTest.progress = e.data.progress;
                this.currentTest.step = e.data.step;
                this.render();
            }
        });

        sse.on('harness', 'test_completed', (e) => {
            if (e.data) {
                this.results.unshift(e.data);
                if (this.results.length > 20) {
                    this.results.pop();
                }
            }
            this.currentTest = null;
            this.render();
        });

        sse.on('harness', 'test_stopped', () => {
            this.currentTest = null;
            this.render();
        });

        sse.on('harness', 'tests_refreshed', (e) => {
            this.isRefreshing = false;
            this.loadTests();
        });

        sse.on('harness', 'queue_updated', (e) => {
            this.loadQueue();
        });
    },

    async refresh() {
        // Load current test first to avoid race conditions with queue rendering
        await this.loadCurrentTest();
        await Promise.all([
            this.loadTests(),
            this.loadResults(),
            this.loadQueue()
        ]);
    },

    async loadQueue() {
        try {
            const data = await fetch(`${api.harnessUrl}/api/tests/queue`).then(r => r.json());
            this.queue = data.queue || [];
            this.queuePaused = data.paused || false;
            this.renderQueue();
        } catch (err) {
            console.error('Failed to load queue:', err);
        }
    },

    async loadResults() {
        try {
            const results = await fetch(`${api.harnessUrl}/api/tests/results`).then(r => r.json());
            if (Array.isArray(results)) {
                this.results = results;
                this.renderResults();
            }
        } catch (err) {
            console.error('Failed to load test results:', err);
        }
    },

    async loadTests() {
        try {
            this.tests = await API.harness.tests();
            this.render();
        } catch (err) {
            console.error('Failed to load tests:', err);
        }
    },

    async loadCurrentTest() {
        try {
            this.currentTest = await API.harness.currentTest();
            if (this.currentTest && !this.currentTest.name) {
                this.currentTest = null;
            }
            this.render();
        } catch (err) {
            this.currentTest = null;
        }
    },

    render() {
        this.renderCurrentTest();
        this.renderQueue();
        this.renderTestList();
        this.renderResults();
    },

    renderQueue() {
        const container = document.getElementById('test-queue');
        if (!container) return;

        // Hide if nothing in queue and nothing running
        if (this.queue.length === 0 && !this.currentTest) {
            container.classList.add('hidden');
            return;
        }

        // Also hide if queue is empty (test is running but nothing pending)
        if (this.queue.length === 0) {
            container.classList.add('hidden');
            return;
        }

        container.classList.remove('hidden');

        const pauseBtn = this.queuePaused
            ? `<button class="btn btn-small btn-primary" onclick="harnessPanel.resumeQueue()">▶ Resume</button>`
            : `<button class="btn btn-small btn-secondary" onclick="harnessPanel.pauseQueue()">⏸ Pause</button>`;

        container.innerHTML = `
            <div class="queue-header">
                <span class="queue-title">Queue (${this.queue.length} pending)</span>
                <div class="queue-controls">
                    ${pauseBtn}
                    <button class="btn btn-small btn-danger" onclick="harnessPanel.clearQueue()">✕ Clear</button>
                </div>
            </div>
            <div class="queue-items">
                ${this.queue.slice(0, 5).map((t, i) => `
                    <span class="queue-item">${i + 1}. ${this.escapeHtml(t.name)}</span>
                `).join('')}
                ${this.queue.length > 5 ? `<span class="queue-more">+${this.queue.length - 5} more...</span>` : ''}
            </div>
        `;
    },

    async pauseQueue() {
        try {
            await fetch(`${api.harnessUrl}/api/tests/queue`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'pause' })
            });
            this.queuePaused = true;
            this.renderQueue();
        } catch (err) {
            console.error('Failed to pause queue:', err);
        }
    },

    async resumeQueue() {
        try {
            await fetch(`${api.harnessUrl}/api/tests/queue`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'resume' })
            });
            this.queuePaused = false;
            this.renderQueue();
        } catch (err) {
            console.error('Failed to resume queue:', err);
        }
    },

    async clearQueue() {
        try {
            await fetch(`${api.harnessUrl}/api/tests/queue`, { method: 'DELETE' });
            this.queue = [];
            this.renderQueue();
        } catch (err) {
            console.error('Failed to clear queue:', err);
        }
    },

    renderCurrentTest() {
        const overlay = document.getElementById('test-running-overlay');
        if (!overlay) return;

        // Only show overlay when on Tests tab
        const testsTab = document.getElementById('tests-panel');
        const isTestsTabActive = testsTab && !testsTab.classList.contains('hidden');

        if (!this.currentTest || !isTestsTabActive) {
            overlay.classList.add('hidden');
            return;
        }

        overlay.classList.remove('hidden');

        const nameEl = overlay.querySelector('.test-running-name');
        const progressEl = overlay.querySelector('.test-running-progress');

        if (nameEl) {
            nameEl.textContent = `Running: ${this.currentTest.name}`;
        }
        if (progressEl) {
            let progressText = '';
            if (this.currentTest.step) {
                progressText = this.currentTest.step;
            }
            if (this.currentTest.progress !== undefined) {
                progressText += ` (${this.currentTest.progress}%)`;
            }
            progressEl.textContent = progressText;
        }
    },

    renderTestList() {
        const container = document.getElementById('test-list');
        if (!container) return;

        // Render header with refresh button
        const headerHtml = `
            <div class="test-list-header">
                <span class="test-count">${this.tests.length} tests</span>
                <button class="btn btn-small btn-secondary"
                        onclick="harnessPanel.refreshTests()"
                        ${this.isRefreshing ? 'disabled' : ''}>
                    ${this.isRefreshing ? '⟳ Refreshing...' : '⟳ Refresh'}
                </button>
            </div>
        `;

        if (this.tests.length === 0) {
            container.innerHTML = headerHtml + '<div class="loading">Discovering tests...</div>';
            return;
        }

        // Group tests by group field
        const groups = {};
        const baseGroupOrder = ['integration', 'unit', 'benchmark', 'simulation'];
        const groupLabels = {
            'integration': 'Integration Tests',
            'benchmark': 'Benchmarks',
            'simulation': 'Live Simulations'
        };

        for (const t of this.tests) {
            const group = t.group || 'other';
            if (!groups[group]) groups[group] = [];
            groups[group].push(t);
        }

        // Build dynamic group order: base groups + unit subgroups sorted alphabetically
        const unitGroups = Object.keys(groups)
            .filter(g => g.startsWith('unit/'))
            .sort();
        const groupOrder = [];
        for (const base of baseGroupOrder) {
            if (base === 'unit') {
                // Insert unit subgroups here
                groupOrder.push(...unitGroups);
            } else if (groups[base]) {
                groupOrder.push(base);
            }
        }

        // Track expanded state (default: integration expanded, others collapsed)
        if (!this.expandedGroups) {
            this.expandedGroups = { integration: true };
        }

        let html = headerHtml;
        for (const groupKey of groupOrder) {
            const tests = groups[groupKey];
            if (!tests || tests.length === 0) continue;

            const isExpanded = this.expandedGroups[groupKey];
            // Generate label: "Unit Tests (api)" for "unit/api"
            let label = groupLabels[groupKey];
            if (!label && groupKey.startsWith('unit/')) {
                const pkg = groupKey.substring(5); // remove "unit/"
                label = `Unit Tests (${pkg})`;
            }
            label = label || groupKey;
            const icon = isExpanded ? '▼' : '▶';

            html += `
                <div class="test-group">
                    <div class="test-group-header" onclick="harnessPanel.toggleGroup('${groupKey}')">
                        <span class="expand-icon">${icon}</span>
                        <span class="group-label">${label}</span>
                        <span class="group-count">(${tests.length})</span>
                        <button class="btn btn-small btn-secondary"
                                onclick="event.stopPropagation(); harnessPanel.runGroup('${groupKey}')"
                                ${this.currentTest ? 'disabled' : ''}>
                            Run All
                        </button>
                    </div>
                    <div class="test-group-items ${isExpanded ? '' : 'collapsed'}">
                        ${tests.map(t => `
                            <div class="test-item ${this.selectedTest === t.name ? 'selected' : ''}"
                                 onclick="harnessPanel.selectTest('${this.escapeHtml(t.name)}')">
                                <div class="test-info">
                                    <div class="test-name">${this.escapeHtml(t.name)}</div>
                                    <div class="test-meta">${this.escapeHtml(t.description || '')}</div>
                                </div>
                                <div class="test-actions">
                                    <button class="btn btn-primary btn-small"
                                            onclick="event.stopPropagation(); harnessPanel.runTest('${this.escapeHtml(t.name)}')"
                                            ${this.currentTest ? 'disabled' : ''}>
                                        Run
                                    </button>
                                </div>
                            </div>
                        `).join('')}
                    </div>
                </div>
            `;
        }

        container.innerHTML = html;
    },

    async refreshTests() {
        this.isRefreshing = true;
        this.renderTestList();

        try {
            await fetch(`${api.harnessUrl}/api/tests/refresh`, { method: 'POST' });
        } catch (err) {
            console.error('Failed to refresh tests:', err);
            this.isRefreshing = false;
            this.renderTestList();
        }
    },

    toggleGroup(groupKey) {
        if (!this.expandedGroups) this.expandedGroups = {};
        this.expandedGroups[groupKey] = !this.expandedGroups[groupKey];
        this.renderTestList();
    },

    async runGroup(groupKey) {
        // Add all tests from group to the queue
        try {
            await fetch(`${api.harnessUrl}/api/tests/queue`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ group: groupKey })
            });
            this.loadQueue();
        } catch (err) {
            alert(`Failed to queue tests: ${err.message}`);
        }
    },

    renderResults() {
        // Use the correct element ID from the new HTML structure
        const container = document.getElementById('results-list');
        if (!container) return;

        if (this.results.length === 0) {
            container.innerHTML = '<div class="loading">No test results yet</div>';
            return;
        }

        // Find group for each result by matching name to tests
        const testsByName = {};
        for (const t of this.tests) {
            testsByName[t.name] = t;
        }

        container.innerHTML = this.results.map((r, idx) => {
            // Duration is in milliseconds from API
            const durationSec = r.duration ? (r.duration / 1000).toFixed(2) : null;
            const test = testsByName[r.name];
            const groupBadge = test?.group ? `<span class="group-badge ${test.group.split('/')[0]}">${test.group.split('/').pop()}</span>` : '';
            const selected = this.selectedResult === idx ? 'selected' : '';
            return `
                <div class="result-item ${r.status} ${selected}"
                     onclick="harnessPanel.selectResult(${idx})">
                    ${groupBadge}
                    <strong>${this.escapeHtml(r.name)}</strong> -
                    <span class="status">${r.status}</span>
                    ${durationSec ? ` (${durationSec}s)` : ''}
                    ${r.trace_id ? ' <span class="trace-icon" title="Has trace">📊</span>' : ''}
                    ${r.error ? `<br><small style="color: var(--error)">${this.escapeHtml(r.error)}</small>` : ''}
                </div>
            `;
        }).join('');
    },

    viewTrace(resultIndex) {
        const result = this.results[resultIndex];
        if (!result) return;

        // Switch to traces tab
        const tracesTab = document.querySelector('[data-tab="traces"]');
        if (tracesTab) {
            tracesTab.click();
        }

        // Load trace - use trace_id if available, otherwise try latest
        setTimeout(() => {
            if (result.trace_id) {
                tracesPanel.loadFromAPI(result.trace_id);
            } else {
                tracesPanel.loadFromAPI();
            }
        }, 100);
    },

    async runTest(name) {
        try {
            await API.harness.runTest(name);
            await this.loadCurrentTest();
        } catch (err) {
            alert(`Failed to start test: ${err.message}`);
        }
    },

    async stopTest() {
        try {
            await API.harness.stopTest();
            this.currentTest = null;
            this.render();
        } catch (err) {
            alert(`Failed to stop test: ${err.message}`);
        }
    },

    async selectTest(name) {
        this.selectedTest = name;
        this.selectedResult = null;
        this.renderTestList();
        this.renderResults();
        await this.loadTestDetails(name);
    },

    async selectResult(index) {
        this.selectedResult = index;
        this.selectedTest = null;
        this.renderTestList();
        this.renderResults();
        await this.loadResultDetails(index);
    },

    async loadTestDetails(name) {
        const container = document.getElementById('test-details-content');
        if (!container) return;

        container.innerHTML = '<div class="loading">Loading source...</div>';

        try {
            const response = await fetch(`${api.harnessUrl}/api/tests/source/${encodeURIComponent(name)}`);
            const data = await response.json();

            if (!response.ok) {
                container.innerHTML = `<div class="details-placeholder">Error: ${data.error || 'Failed to load'}</div>`;
                return;
            }

            let html = `
                <div class="test-detail-header">
                    <div>
                        <div class="test-detail-name">${this.escapeHtml(data.name)}</div>
                        <div class="test-detail-meta">
                            ${data.group ? `<span class="group-badge ${data.group.split('/')[0]}">${data.group}</span>` : ''}
                            ${data.package ? `<span>${this.escapeHtml(data.package)}</span>` : ''}
                        </div>
                    </div>
                    <button class="btn btn-primary btn-small"
                            onclick="harnessPanel.runTest('${this.escapeHtml(data.name)}')"
                            ${this.currentTest ? 'disabled' : ''}>
                        Run Test
                    </button>
                </div>
            `;

            if (data.description) {
                html += `
                    <div class="test-detail-section">
                        <h4>Description</h4>
                        <p>${this.escapeHtml(data.description)}</p>
                    </div>
                `;
            }

            if (data.has_source && data.file_path) {
                html += `
                    <div class="test-detail-section">
                        <h4>Location</h4>
                        <div class="test-detail-meta">
                            <a href="${data.file_uri}:${data.line}" title="Open in editor">${this.escapeHtml(data.file_path)}:${data.line}</a>
                        </div>
                    </div>
                `;
            }

            if (data.has_source && (data.source_html || data.source)) {
                html += `
                    <div class="test-detail-section">
                        <h4>Source Code</h4>
                        <pre class="source-code">${data.source_html || this.escapeHtml(data.source)}</pre>
                    </div>
                `;
            } else if (!data.has_source) {
                html += `
                    <div class="test-detail-section">
                        <p class="details-placeholder">No source code available (special test)</p>
                    </div>
                `;
            }

            container.innerHTML = html;
        } catch (err) {
            container.innerHTML = `<div class="details-placeholder">Failed to load: ${err.message}</div>`;
        }
    },

    async loadResultDetails(index) {
        const container = document.getElementById('test-details-content');
        if (!container) return;

        container.innerHTML = '<div class="loading">Loading result...</div>';

        try {
            const response = await fetch(`${api.harnessUrl}/api/tests/results?index=${index}`);
            const data = await response.json();

            if (!response.ok) {
                container.innerHTML = `<div class="details-placeholder">Error: ${data.error || 'Failed to load'}</div>`;
                return;
            }

            const durationSec = data.duration ? (data.duration / 1000).toFixed(2) : null;
            const testName = this.escapeHtml(data.name);

            let html = `
                <div class="test-detail-header">
                    <div>
                        <div class="test-detail-name">${testName}</div>
                        <div class="test-detail-meta">
                            <span class="result-status ${data.status}">${data.status}</span>
                            ${durationSec ? `<span>${durationSec}s</span>` : ''}
                            <span>${data.timestamp || ''}</span>
                        </div>
                    </div>
                    <div>
                        <button class="btn btn-small" onclick="harnessPanel.selectTest('${testName}')">Go to Test</button>
                        ${data.trace_id ? `<button class="btn btn-small" onclick="harnessPanel.viewTraceById(${data.trace_id})">View Trace 📊</button>` : ''}
                        <button class="btn btn-primary btn-small"
                                onclick="harnessPanel.runTest('${testName}')"
                                ${this.currentTest ? 'disabled' : ''}>
                            Run Again
                        </button>
                    </div>
                </div>
            `;

            if (data.error) {
                html += `
                    <div class="test-detail-section">
                        <h4>Error</h4>
                        <div style="color: var(--error)">${this.escapeHtml(data.error)}</div>
                    </div>
                `;
            }

            if (data.output) {
                html += `
                    <div class="test-detail-section">
                        <h4>Output</h4>
                        <pre class="test-output-log">${this.escapeHtml(data.output)}</pre>
                    </div>
                `;
            } else {
                html += `
                    <div class="test-detail-section">
                        <p class="details-placeholder">No output captured</p>
                    </div>
                `;
            }

            // Collapsible source code section
            html += `
                <div class="test-detail-section collapsible-section">
                    <h4 class="collapsible-header" onclick="harnessPanel.toggleSourceCode('${testName}')">
                        <span class="collapse-icon">▶</span> Source Code
                    </h4>
                    <div class="collapsible-content collapsed" id="result-source-content">
                        <div class="loading">Click to load source...</div>
                    </div>
                </div>
            `;

            container.innerHTML = html;
        } catch (err) {
            container.innerHTML = `<div class="details-placeholder">Failed to load: ${err.message}</div>`;
        }
    },

    async toggleSourceCode(testName) {
        const content = document.getElementById('result-source-content');
        const header = content?.previousElementSibling;
        const icon = header?.querySelector('.collapse-icon');

        if (!content) return;

        const isCollapsed = content.classList.contains('collapsed');

        if (isCollapsed) {
            // Expand and load source if not already loaded
            content.classList.remove('collapsed');
            if (icon) icon.textContent = '▼';

            if (content.querySelector('.loading')) {
                // Load source code
                try {
                    const response = await fetch(`${api.harnessUrl}/api/tests/source/${encodeURIComponent(testName)}`);
                    const data = await response.json();

                    if (data.has_source && (data.source_html || data.source)) {
                        content.innerHTML = `
                            <div class="test-detail-meta" style="margin-bottom: 0.5rem">
                                <a href="${data.file_uri}:${data.line}" title="Open in editor">${this.escapeHtml(data.file_path)}:${data.line}</a>
                            </div>
                            <pre class="source-code">${data.source_html || this.escapeHtml(data.source)}</pre>
                        `;
                    } else {
                        content.innerHTML = '<p class="details-placeholder">No source code available</p>';
                    }
                } catch (err) {
                    content.innerHTML = `<p class="details-placeholder">Failed to load source: ${err.message}</p>`;
                }
            }
        } else {
            // Collapse
            content.classList.add('collapsed');
            if (icon) icon.textContent = '▶';
        }
    },

    viewTraceById(traceId) {
        // Switch to traces tab
        const tracesTab = document.querySelector('[data-tab="traces"]');
        if (tracesTab) {
            tracesTab.click();
        }

        setTimeout(() => {
            tracesPanel.loadFromAPI(traceId);
        }, 100);
    },

    escapeHtml(text) {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
};
