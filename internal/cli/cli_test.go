package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeDoesNotNeedACluster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")
	body := []byte(`{
	  "schemaVersion": "v1alpha1",
	  "cluster": {"name": "fixture", "kubernetesVersion": "1.32.2"},
	  "nodes": [{"name": "cp", "role": "control-plane", "kubeletVersion": "1.32.2"}]
	}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Execute([]string{"analyze", "--inventory", path, "--target-kubernetes", "1.33"}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "insufficient-evidence") && !strings.Contains(stdout.String(), "plan-available") && !strings.Contains(stdout.String(), "compatible") {
		t.Fatalf("stdout:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "verified compatible") && strings.Contains(stdout.String(), "cilium") {
		t.Fatal("analysis invented a component result")
	}
}
