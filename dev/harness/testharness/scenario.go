// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package testharness

import (
	"fmt"
	"time"

	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/fakek8s"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/harness/step"
	"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/pkg/crdmeta"
)

// Scenario defines a test scenario with ordered steps.
type Scenario struct {
	Name        string
	Description string
	Steps       []ScenarioStep
}

// ScenarioStep is a single step in a scenario.
type ScenarioStep struct {
	// Action describes what this step does (for logging)
	Action string

	// Wait specifies which worker/point to wait for (optional)
	WaitForWorker string
	WaitForPoint  step.Point

	// Release specifies which worker to release after wait (optional)
	ReleaseWorker string
	ReleaseAll    bool

	// Verify is called after wait/release to verify state (optional)
	Verify func(h *Harness) error

	// Inject is called to inject events/faults before proceeding
	Inject func(h *Harness) error
}

// ScenarioResult holds the result of running a scenario.
type ScenarioResult struct {
	Name      string
	Duration  time.Duration
	StepCount int
	Passed    bool
	Error     error
	StepLog   []step.LogEntry
}

// RunScenario executes a test scenario.
func (h *Harness) RunScenario(s Scenario) ScenarioResult {
	start := time.Now()
	result := ScenarioResult{
		Name: s.Name,
	}

	for i, step := range s.Steps {
		result.StepCount = i + 1

		// Inject
		if step.Inject != nil {
			if err := step.Inject(h); err != nil {
				result.Error = fmt.Errorf("step %d (%s) inject: %w", i, step.Action, err)
				result.StepLog = h.StepLog()
				result.Duration = time.Since(start)
				return result
			}
		}

		// Wait
		if step.WaitForWorker != "" {
			_, err := h.WaitFor(step.WaitForWorker, 5*time.Second)
			if err != nil {
				result.Error = fmt.Errorf("step %d (%s) wait for worker %q: %w", i, step.Action, step.WaitForWorker, err)
				result.StepLog = h.StepLog()
				result.Duration = time.Since(start)
				return result
			}
		}
		if step.WaitForPoint != "" {
			_, err := h.WaitForPoint(step.WaitForPoint, 5*time.Second)
			if err != nil {
				result.Error = fmt.Errorf("step %d (%s) wait for point %q: %w", i, step.Action, step.WaitForPoint, err)
				result.StepLog = h.StepLog()
				result.Duration = time.Since(start)
				return result
			}
		}

		// Release
		if step.ReleaseWorker != "" {
			if err := h.Release(step.ReleaseWorker); err != nil {
				result.Error = fmt.Errorf("step %d (%s) release %q: %w", i, step.Action, step.ReleaseWorker, err)
				result.StepLog = h.StepLog()
				result.Duration = time.Since(start)
				return result
			}
		}
		if step.ReleaseAll {
			h.ReleaseAll()
		}

		// Verify
		if step.Verify != nil {
			if err := step.Verify(h); err != nil {
				result.Error = fmt.Errorf("step %d (%s) verify: %w", i, step.Action, err)
				result.StepLog = h.StepLog()
				result.Duration = time.Since(start)
				return result
			}
		}
	}

	result.Passed = true
	result.StepLog = h.StepLog()
	result.Duration = time.Since(start)
	return result
}

// ExampleScenarios provides example scenarios for reference.
var ExampleScenarios = []Scenario{
	{
		Name:        "basic-state-flow",
		Description: "Test basic domain state observation and export flow",
		Steps: []ScenarioStep{
			{
				Action:        "Wait for state consumer to be ready",
				WaitForWorker: "state-consumer",
				WaitForPoint:  "state:pre-process",
			},
			{
				Action:        "Release state consumer, wait for create",
				ReleaseWorker: "state-consumer",
				WaitForPoint:  "state:create",
			},
			{
				Action:        "Release create, wait for k8s export",
				ReleaseWorker: "state-consumer",
				WaitForWorker: "k8s-exporter",
				WaitForPoint:  "exporter:create",
			},
			{
				Action: "Verify resource created",
				Verify: func(h *Harness) error {
					// Check that K8s resource was created
					// (specific verification would go here)
					return nil
				},
			},
			{
				Action:     "Release all and complete",
				ReleaseAll: true,
			},
		},
	},
	{
		Name:        "k8s-failure-recovery",
		Description: "Test that operator handles K8s API failures gracefully",
		Steps: []ScenarioStep{
			{
				Action: "Inject K8s failure",
				Inject: func(h *Harness) error {
					h.SetK8sFault(crdmeta.BaseLibvirtDomainResource, &fakek8s.FaultConfig{
						Code:     503,
						ErrorMsg: "service unavailable",
					})
					return nil
				},
			},
			{
				Action:        "Wait for export attempt",
				WaitForWorker: "k8s-exporter",
				WaitForPoint:  "exporter:create",
			},
			{
				Action: "Clear fault and release",
				Inject: func(h *Harness) error {
					h.SetK8sFault(crdmeta.BaseLibvirtDomainResource, nil)
					return nil
				},
				ReleaseAll: true,
			},
		},
	},
}

