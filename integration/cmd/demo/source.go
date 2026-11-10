// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// TestSource holds source code information for a test.
type TestSource struct {
	FilePath       string `json:"file_path"`
	Line           int    `json:"line"`
	EndLine        int    `json:"end_line"`
	Source         string `json:"source"`
	SourceHTML     string `json:"source_html"` // Syntax highlighted HTML
	FileURI        string `json:"file_uri"`    // file:// URI for clickable link
}

// TestSourceIndex indexes test function locations.
type TestSourceIndex struct {
	repoRoot string
	// testName -> TestSource (without source content, just location)
	locations map[string]*TestSource
}

// NewTestSourceIndex creates a new source index.
func NewTestSourceIndex(repoRoot string) *TestSourceIndex {
	return &TestSourceIndex{
		repoRoot:  repoRoot,
		locations: make(map[string]*TestSource),
	}
}

// IndexPackage scans a package directory for test functions.
func (idx *TestSourceIndex) IndexPackage(pkgDir string) error {
	// Find all _test.go files
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		filePath := filepath.Join(pkgDir, entry.Name())
		if err := idx.indexFile(filePath); err != nil {
			// Log but continue with other files
			continue
		}
	}

	return nil
}

// indexFile parses a single test file and indexes test functions.
func (idx *TestSourceIndex) indexFile(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// Check if it's a test function
		if !strings.HasPrefix(fn.Name.Name, "Test") {
			continue
		}

		// Get position info
		startPos := fset.Position(fn.Pos())
		endPos := fset.Position(fn.End())

		absPath, _ := filepath.Abs(filePath)

		idx.locations[fn.Name.Name] = &TestSource{
			FilePath: absPath,
			Line:     startPos.Line,
			EndLine:  endPos.Line,
			FileURI:  "file://" + absPath,
		}
	}

	return nil
}

// GetLocation returns the location of a test function (without source).
func (idx *TestSourceIndex) GetLocation(testName string) *TestSource {
	return idx.locations[testName]
}

// GetSource returns full source information including the source code.
func (idx *TestSourceIndex) GetSource(testName string) (*TestSource, error) {
	loc := idx.locations[testName]
	if loc == nil {
		return nil, nil
	}

	// Read the source file and extract the function
	source, err := extractFunctionSource(loc.FilePath, testName)
	if err != nil {
		return nil, err
	}

	// Generate syntax-highlighted HTML
	sourceHTML := highlightGoSource(source)

	// Return a copy with source populated
	return &TestSource{
		FilePath:   loc.FilePath,
		Line:       loc.Line,
		EndLine:    loc.EndLine,
		Source:     source,
		SourceHTML: sourceHTML,
		FileURI:    loc.FileURI,
	}, nil
}

// extractFunctionSource extracts a function's source code from a file.
func extractFunctionSource(filePath, funcName string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		return "", err
	}

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		if fn.Name.Name != funcName {
			continue
		}

		// Extract source from content
		startOffset := fset.Position(fn.Pos()).Offset
		endOffset := fset.Position(fn.End()).Offset

		if startOffset >= 0 && endOffset <= len(content) {
			return string(content[startOffset:endOffset]), nil
		}
	}

	return "", nil
}

// IndexFromDiscovery indexes all packages found in test discovery.
func (idx *TestSourceIndex) IndexFromDiscovery(tests []TestInfo) {
	// Collect unique package directories
	indexed := make(map[string]bool)

	for _, t := range tests {
		pkgDir := idx.resolvePackageDir(t.Package)
		if pkgDir == "" || indexed[pkgDir] {
			continue
		}
		indexed[pkgDir] = true
		idx.IndexPackage(pkgDir)
	}
}

// resolvePackageDir converts a Go import path to a directory path.
func (idx *TestSourceIndex) resolvePackageDir(pkg string) string {
	// Strip known module prefixes
	relPath := pkg
	prefixes := []string{
		"github.com/cobaltcore-dev/libvirt-kubernetes-exporter/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(pkg, prefix) {
			relPath = strings.TrimPrefix(pkg, prefix)
			break
		}
	}

	// Construct full path
	fullPath := filepath.Join(idx.repoRoot, relPath)
	if info, err := os.Stat(fullPath); err == nil && info.IsDir() {
		return fullPath
	}

	return ""
}

// highlightGoSource applies syntax highlighting to Go source code.
// Returns HTML with spans for different token types.
func highlightGoSource(source string) string {
	var result strings.Builder
	fset := token.NewFileSet()

	// Create a scanner for the source
	var s scanner.Scanner
	file := fset.AddFile("", fset.Base(), len(source))
	s.Init(file, []byte(source), nil, scanner.ScanComments)

	lastOffset := 0
	srcBytes := []byte(source)

	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}

		offset := fset.Position(pos).Offset
		if offset < 0 || offset > len(srcBytes) {
			continue
		}

		// Write any text between last token and this one (whitespace, etc.)
		if offset > lastOffset {
			result.WriteString(html.EscapeString(string(srcBytes[lastOffset:offset])))
		}

		// Determine the CSS class and literal to use
		var class string
		var text string

		switch {
		case tok.IsKeyword():
			class = "kw" // keyword
			text = tok.String()
		case tok == token.IDENT:
			// Check for built-in types and functions
			if isBuiltin(lit) {
				class = "bi" // builtin
			}
			text = lit
		case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
			class = "num" // number
			text = lit
		case tok == token.STRING || tok == token.CHAR:
			class = "str" // string
			text = lit
		case tok == token.COMMENT:
			class = "cmt" // comment
			text = lit
		default:
			// Operators and punctuation - no highlighting
			if lit != "" {
				text = lit
			} else {
				text = tok.String()
			}
		}

		// Write the token with or without span
		if class != "" {
			result.WriteString(`<span class="`)
			result.WriteString(class)
			result.WriteString(`">`)
			result.WriteString(html.EscapeString(text))
			result.WriteString("</span>")
		} else {
			result.WriteString(html.EscapeString(text))
		}

		// Update lastOffset
		if lit != "" {
			lastOffset = offset + len(lit)
		} else {
			lastOffset = offset + len(tok.String())
		}
	}

	// Write any remaining text
	if lastOffset < len(srcBytes) {
		result.WriteString(html.EscapeString(string(srcBytes[lastOffset:])))
	}

	return result.String()
}

// isBuiltin checks if an identifier is a Go builtin type or function.
func isBuiltin(name string) bool {
	builtins := map[string]bool{
		// Types
		"bool": true, "byte": true, "complex64": true, "complex128": true,
		"error": true, "float32": true, "float64": true,
		"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"rune": true, "string": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
		"uintptr": true, "any": true, "comparable": true,
		// Functions
		"append": true, "cap": true, "clear": true, "close": true, "complex": true,
		"copy": true, "delete": true, "imag": true, "len": true, "make": true,
		"max": true, "min": true, "new": true, "panic": true, "print": true,
		"println": true, "real": true, "recover": true,
		// Constants
		"true": true, "false": true, "nil": true, "iota": true,
	}
	return builtins[name]
}
