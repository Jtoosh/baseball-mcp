package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the public deployment build command in a minimal module. The actual
// Go compiler verifies that the host race gate survives the builder's CGO=0.
func TestHostedBuildGateSupportsCGODisabledDeploymentBuilder(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Mkdir(filepath.Join(dir, "static"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"go.mod": "module buildfixture\n\ngo 1.26.6\n", "main.go": "package main\nfunc main() {}\n", "static/app.js": "'use strict';\n"}
	for name, body := range files {
		if err = os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("bash", filepath.Join(root, "scripts", "build-hosted.sh"))
	command.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CGO_ENABLED=") && !strings.HasPrefix(entry, "VERCEL_OUTPUT_FILE=") && !strings.HasPrefix(entry, "GOOS=") && !strings.HasPrefix(entry, "GOARCH=") {
			command.Env = append(command.Env, entry)
		}
	}
	output := filepath.Join(dir, "server")
	command.Env = append(command.Env, "CGO_ENABLED=0", "TEST_DATABASE_URL=fixture-not-used", "GOWORK=off", "VERCEL_OUTPUT_FILE="+output)
	if body, err := command.CombinedOutput(); err != nil {
		t.Fatalf("deployment build gate failed with CGO disabled: %v\n%s", err, body)
	}
	if _, err = os.Stat(output); err != nil {
		t.Fatalf("deployment binary missing: %v", err)
	}
}
