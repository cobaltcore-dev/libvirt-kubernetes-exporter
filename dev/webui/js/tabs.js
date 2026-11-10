// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// Tab navigation system
const tabs = {
    activeTab: 'overview',

    init() {
        // Main tab navigation
        document.querySelectorAll('.main-tab').forEach(btn => {
            btn.addEventListener('click', () => {
                this.switchTo(btn.dataset.tab, true);
            });
        });

        // Sub-tab navigation (within panels)
        document.querySelectorAll('.sub-tab').forEach(btn => {
            btn.addEventListener('click', () => {
                this.switchSubTab(btn);
            });
        });

        // Handle browser back/forward
        window.addEventListener('hashchange', () => {
            const tab = this.getTabFromHash();
            if (tab && tab !== this.activeTab) {
                this.switchTo(tab, false);
            }
        });

        // Show initial tab from URL hash or default
        const initialTab = this.getTabFromHash() || this.activeTab;
        this.switchTo(initialTab, false);
    },

    getTabFromHash() {
        const hash = window.location.hash.slice(1); // Remove #
        const validTabs = ['overview', 'libvirt', 'operator', 'k8s', 'tests', 'traces'];
        return validTabs.includes(hash) ? hash : null;
    },

    switchTo(tabName, updateHash = true) {
        this.activeTab = tabName;

        // Update URL hash (without triggering hashchange)
        if (updateHash) {
            history.pushState(null, '', '#' + tabName);
        }

        // Update main tab buttons
        document.querySelectorAll('.main-tab').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.tab === tabName);
        });

        // Update tab panels
        document.querySelectorAll('.tab-panel').forEach(panel => {
            panel.classList.toggle('hidden', panel.id !== `${tabName}-panel`);
        });

        // Emit event for panel-specific initialization
        window.dispatchEvent(new CustomEvent('tabchange', { detail: { tab: tabName } }));

        // Trigger resize for canvas-based components
        if (tabName === 'traces' && typeof tracesPanel !== 'undefined') {
            setTimeout(() => {
                if (tracesPanel.timeline) {
                    tracesPanel.timeline.resize();
                }
            }, 0);
        }

        // Update test running overlay visibility (only show on tests tab)
        if (typeof harnessPanel !== 'undefined') {
            harnessPanel.renderCurrentTest();
        }
    },

    switchSubTab(btn) {
        const subtabId = btn.dataset.subtab;
        const container = btn.closest('.tab-panel');

        // Update sub-tab buttons in this container
        container.querySelectorAll('.sub-tab').forEach(t => {
            t.classList.toggle('active', t === btn);
        });

        // Update sub-panels
        container.querySelectorAll('.sub-panel').forEach(panel => {
            panel.classList.toggle('hidden', panel.id !== subtabId);
        });
    },

    // Get the currently active tab
    getActive() {
        return this.activeTab;
    }
};
