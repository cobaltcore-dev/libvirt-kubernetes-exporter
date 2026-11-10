// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

// SSE (Server-Sent Events) connection manager

class SSEManager {
    constructor() {
        this.connections = {};
        this.handlers = {};
        this.retryTimeouts = {};
    }

    // Connect to a component's event stream
    connect(component, url) {
        // Clean up existing connection and pending retry
        if (this.connections[component]) {
            this.connections[component].close();
            delete this.connections[component];
        }
        if (this.retryTimeouts[component]) {
            clearTimeout(this.retryTimeouts[component]);
            delete this.retryTimeouts[component];
        }

        console.log(`SSE: Connecting to ${component} at ${url}`);

        const source = new EventSource(url);
        this.connections[component] = source;

        source.onopen = () => {
            console.log(`SSE: ${component} connected`);
            this.updateStatus(component, true);
        };

        source.onerror = () => {
            // Only handle if connection is actually closed/failed
            // EventSource can fire onerror for non-fatal issues while still connected
            if (source.readyState === EventSource.CLOSED) {
                this.updateStatus(component, false);

                // Clean up if this source is still current
                if (this.connections[component] === source) {
                    delete this.connections[component];
                }

                // Schedule retry if not already pending
                if (!this.retryTimeouts[component]) {
                    console.warn(`SSE: ${component} disconnected, will retry in 5s...`);
                    this.retryTimeouts[component] = setTimeout(() => {
                        delete this.retryTimeouts[component];
                        this.connect(component, url);
                    }, 5000);
                }
            } else if (source.readyState === EventSource.CONNECTING) {
                // Still trying to connect, update status but let it retry automatically
                this.updateStatus(component, false);
            }
            // If OPEN, ignore the error - connection is still good
        };

        source.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                this.dispatch(component, data);
            } catch (err) {
                console.error(`SSE: ${component} parse error`, err);
            }
        };
    }

    // Disconnect from a component
    disconnect(component) {
        if (this.connections[component]) {
            this.connections[component].close();
            delete this.connections[component];
            this.updateStatus(component, false);
        }
    }

    // Register an event handler
    on(component, eventType, handler) {
        const key = `${component}:${eventType}`;
        if (!this.handlers[key]) {
            this.handlers[key] = [];
        }
        this.handlers[key].push(handler);
    }

    // Register a handler for all events from a component
    onAny(component, handler) {
        const key = `${component}:*`;
        if (!this.handlers[key]) {
            this.handlers[key] = [];
        }
        this.handlers[key].push(handler);
    }

    // Dispatch event to handlers
    dispatch(component, event) {
        // Call specific handlers
        const specificKey = `${component}:${event.type}`;
        if (this.handlers[specificKey]) {
            this.handlers[specificKey].forEach(h => h(event));
        }

        // Call wildcard handlers
        const wildcardKey = `${component}:*`;
        if (this.handlers[wildcardKey]) {
            this.handlers[wildcardKey].forEach(h => h(event));
        }
    }

    // Update connection status indicator
    updateStatus(component, connected) {
        const dot = document.getElementById(`${component}-status`);
        if (dot) {
            if (connected) {
                dot.classList.add('connected');
            } else {
                dot.classList.remove('connected');
            }
        }
    }

    // Check if connected to a component
    isConnected(component) {
        const source = this.connections[component];
        return source && source.readyState === EventSource.OPEN;
    }
}

// Global SSE manager instance
const sse = new SSEManager();
