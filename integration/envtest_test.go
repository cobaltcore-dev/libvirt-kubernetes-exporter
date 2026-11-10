// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"testing"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// envtestCfg is the REST config shared by all envtest tests.
// Populated by TestMain when KUBEBUILDER_ASSETS is set; nil otherwise.
var envtestCfg *rest.Config

func TestMain(m *testing.M) {
	kubebuilderAssets := os.Getenv("KUBEBUILDER_ASSETS")
	if kubebuilderAssets == "" {
		// No envtest assets available — only run non-envtest tests.
		os.Exit(m.Run())
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{"../charts/libvirt-kubernetes-exporter/crds"},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: kubebuilderAssets,
	}

	cfg, err := env.Start()
	if err != nil {
		panic("envtest start: " + err.Error())
	}
	envtestCfg = cfg

	code := m.Run()

	if stopErr := env.Stop(); stopErr != nil {
		panic("envtest stop: " + stopErr.Error())
	}

	os.Exit(code)
}
