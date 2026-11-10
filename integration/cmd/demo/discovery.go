// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// TestDiscovery discovers tests from Go modules.
type TestDiscovery struct {
	repoRoot string
	tests    []TestInfo
	lastScan time.Time
	scanErr  error
	mu       sync.RWMutex
}

// NewTestDiscovery creates a new test discovery instance.
func NewTestDiscovery(repoRoot string) *TestDiscovery {
	return &TestDiscovery{
		repoRoot: repoRoot,
	}
}

// Tests returns the cached list of tests.
func (d *TestDiscovery) Tests() []TestInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]TestInfo, len(d.tests))
	copy(result, d.tests)
	return result
}

// LastScan returns the time of the last scan.
func (d *TestDiscovery) LastScan() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastScan
}

// ScanError returns any error from the last scan.
func (d *TestDiscovery) ScanError() error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.scanErr
}

// Refresh rescans all modules for tests.
func (d *TestDiscovery) Refresh() error {
	tests, err := d.discoverAll()

	d.mu.Lock()
	defer d.mu.Unlock()
	d.tests = tests
	d.lastScan = time.Now()
	d.scanErr = err
	return err
}

// discoverAll finds all go.mod files and discovers tests in each module.
func (d *TestDiscovery) discoverAll() ([]TestInfo, error) {
	modules, err := d.findModules()
	if err != nil {
		return nil, err
	}

	var allTests []TestInfo
	for _, modDir := range modules {
		tests, err := d.discoverInModule(modDir)
		if err != nil {
			// Log but continue with other modules
			continue
		}
		allTests = append(allTests, tests...)
	}

	// Sort by group, then by name
	sort.Slice(allTests, func(i, j int) bool {
		if allTests[i].Group != allTests[j].Group {
			return groupOrder(allTests[i].Group) < groupOrder(allTests[j].Group)
		}
		return allTests[i].Name < allTests[j].Name
	})

	return allTests, nil
}

// groupOrder returns sort priority for groups.
func groupOrder(group string) int {
	switch group {
	case "integration":
		return 0
	case "unit":
		return 1
	case "benchmark":
		return 2
	case "simulation":
		return 3
	default:
		return 99
	}
}

// findModules finds all directories containing go.mod files.
func (d *TestDiscovery) findModules() ([]string, error) {
	var modules []string
	err := filepath.Walk(d.repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.Name() == "go.mod" {
			modules = append(modules, filepath.Dir(path))
		}
		// Skip vendor directories
		if info.IsDir() && info.Name() == "vendor" {
			return filepath.SkipDir
		}
		return nil
	})
	return modules, err
}

// discoverInModule runs go test -list in a module directory.
func (d *TestDiscovery) discoverInModule(modDir string) ([]TestInfo, error) {
	cmd := exec.Command("go", "test", "-list", ".*", "./...")
	cmd.Dir = modDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's just "no test files" which is fine
		if strings.Contains(string(output), "no test files") ||
			strings.Contains(string(output), "[no test files]") {
			// Parse what we can from partial output
		} else if !strings.Contains(string(output), "ok ") {
			return nil, err
		}
	}

	return d.parseTestList(modDir, output)
}

// parseTestList parses the output of go test -list.
func (d *TestDiscovery) parseTestList(modDir string, output []byte) ([]TestInfo, error) {
	var tests []TestInfo
	var currentPkg string

	// Relative path from repo root for group detection
	relModDir, _ := filepath.Rel(d.repoRoot, modDir)
	if relModDir == "" || relModDir == "." {
		relModDir = filepath.Base(modDir)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	testNameRe := regexp.MustCompile(`^Test\w+`)
	okLineRe := regexp.MustCompile(`^ok\s+(\S+)`)
	noTestRe := regexp.MustCompile(`^\?\s+(\S+)\s+\[no test files\]`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		// Check for "ok <package>" line - marks end of package tests
		if matches := okLineRe.FindStringSubmatch(line); matches != nil {
			currentPkg = matches[1]
			continue
		}

		// Check for "? <package> [no test files]"
		if matches := noTestRe.FindStringSubmatch(line); matches != nil {
			continue
		}

		// Check for test name
		if testNameRe.MatchString(line) {
			testName := line
			pkg := currentPkg
			if pkg == "" {
				// Package not yet known, will be set when we see "ok" line
				// For now, use module path as fallback
				pkg = relModDir
			}

			test := d.createTestInfo(testName, pkg, relModDir)
			tests = append(tests, test)
		}
	}

	// Now we need to assign packages properly - go test -list outputs tests
	// before the "ok" line, so we need to re-process
	tests = d.assignPackages(output, tests, relModDir)

	return tests, nil
}

// assignPackages properly assigns package paths to tests.
func (d *TestDiscovery) assignPackages(output []byte, tests []TestInfo, relModDir string) []TestInfo {
	// Re-parse to get proper package assignments
	var result []TestInfo
	var currentTests []string
	var currentPkg string

	scanner := bufio.NewScanner(bytes.NewReader(output))
	testNameRe := regexp.MustCompile(`^Test\w+`)
	okLineRe := regexp.MustCompile(`^ok\s+(\S+)`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if matches := okLineRe.FindStringSubmatch(line); matches != nil {
			currentPkg = matches[1]
			// Assign package to accumulated tests
			for _, testName := range currentTests {
				test := d.createTestInfo(testName, currentPkg, relModDir)
				result = append(result, test)
			}
			currentTests = nil
			continue
		}

		if testNameRe.MatchString(line) {
			currentTests = append(currentTests, line)
		}
	}

	return result
}

// createTestInfo creates a TestInfo from test name and package.
func (d *TestDiscovery) createTestInfo(testName, pkg, relModDir string) TestInfo {
	group := d.deriveGroup(testName, pkg, relModDir)
	description := d.deriveDescription(testName, pkg, group)

	// Extract short package name for display
	shortPkg := pkg
	if idx := strings.LastIndex(pkg, "/"); idx != -1 {
		shortPkg = pkg[idx+1:]
	}

	return TestInfo{
		Name:        testName,
		Description: description,
		TestFunc:    testName,
		Group:       group,
		Package:     pkg,
		ShortPkg:    shortPkg,
	}
}

// deriveGroup determines the test group from name and package.
func (d *TestDiscovery) deriveGroup(testName, pkg, relModDir string) string {
	// Check for special naming patterns first
	if strings.HasPrefix(testName, "TestScenario_") {
		return "integration"
	}
	if strings.HasPrefix(testName, "TestBenchmark") || strings.Contains(testName, "Throughput") {
		return "benchmark"
	}

	// Extract short package name for unit test subgrouping
	shortPkg := pkg
	if idx := strings.LastIndex(pkg, "/"); idx != -1 {
		shortPkg = pkg[idx+1:]
	}

	// Check module/package path
	switch {
	case strings.Contains(relModDir, "integration"):
		// Integration module tests
		// Only a few are true integration tests (full operator + fakes)
		if isIntegrationTest(testName) {
			return "integration"
		}
		// Unit tests get subgrouped by package
		return "unit/" + shortPkg
	case strings.Contains(pkg, "operator/pkg/"):
		return "unit/" + shortPkg
	case strings.Contains(pkg, "fake-libvirt"):
		return "unit/" + shortPkg
	case strings.Contains(pkg, "harness"):
		return "unit/" + shortPkg
	default:
		return "unit/" + shortPkg
	}
}

// isIntegrationTest checks if a test name suggests it's an integration test.
// Integration tests run the full operator with fake-libvirt and fake-k8s.
func isIntegrationTest(name string) bool {
	// Tests that use TestEnv (full operator stack)
	integrationPatterns := []string{
		"TestBasicDomainDiscovery",
		"TestStateChange",
		"TestDomainDeletion",
		"TestScenario_",
	}
	for _, p := range integrationPatterns {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// deriveDescription generates a human-readable description.
func (d *TestDiscovery) deriveDescription(testName, pkg, group string) string {
	// Convert TestCamelCase to "Camel case" description
	name := strings.TrimPrefix(testName, "Test")
	name = strings.TrimPrefix(name, "Scenario_")

	// Insert spaces before capitals, but not between consecutive capitals (acronyms)
	// e.g., "APIComponents" -> "API Components", not "A P I Components"
	var result strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Only add space if previous char was lowercase OR next char is lowercase
			// This keeps acronyms together: "APIStatus" -> "API Status"
			prevLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if prevLower || nextLower {
				result.WriteRune(' ')
			}
		}
		result.WriteRune(r)
	}

	desc := strings.ToLower(result.String())

	// No longer need to prefix with package since it's in the group label
	return desc
}
