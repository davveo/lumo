package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndCheckPreserveWireValuesAndDetectStaleOutput(t *testing.T) {
	directory := t.TempDir()
	source, output := filepath.Join(directory, "enum.go"), filepath.Join(directory, "constants.js")
	declaration := "package constants\n// enum:Probe\nconst (ProbeCreating = \"CREATING\"; ProbeDefault = \"\")\n"
	if err := os.WriteFile(source, []byte(declaration), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(directory, output, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), `"Creating": "CREATING"`) || !strings.Contains(string(before), `"Default": ""`) {
		t.Fatal("wire values or intentional empty enum changed", string(before))
	}
	if err := run(directory, output, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(strings.ReplaceAll(declaration, "CREATING", "PENDING")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(directory, output, true); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatal("stale browser constants were accepted", err)
	}
	after, _ := os.ReadFile(output)
	if string(before) != string(after) {
		t.Fatal("check mode rewrote the deployed script")
	}
}

func TestGeneratorRejectsAmbiguousDefinitionsWithoutWritingOutput(t *testing.T) {
	for name, declaration := range map[string]string{
		"typed":       "const ProbeCreating string = \"CREATING\"",
		"wrong group": "const OrderCreating = \"CREATING\"",
		"expression":  "const ProbeCreating = \"CRE\" + \"ATING\"",
		"implicit":    "const (ProbeCreating = \"CREATING\"; ProbePending)",
		"duplicate":   "const ProbeCreating = \"CREATING\"\n//enum:Probe\nconst ProbePending = \"PENDING\"",
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "enum.go"), []byte("package constants\n//enum:Probe\n"+declaration), 0600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(directory, "constants.js")
			if err := run(directory, output, false); err == nil {
				t.Fatal("invalid enum definition was accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("invalid source generated a browser script", err)
			}
		})
	}
}
