package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpShowsCommandExamples(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{
			"clusterpilot discover -o inventory.json",
			"clusterpilot analyze --inventory inventory.json --target-kubernetes 1.33",
			"clusterpilot plan --inventory inventory.json --target-kubernetes 1.33 -o plan.json",
			"clusterpilot report --input plan.json --format markdown",
		}},
		{[]string{"discover", "--help"}, []string{"clusterpilot discover --context kind-dev"}},
		{[]string{"analyze", "--help"}, []string{"--rules ./extra-rules"}},
		{[]string{"plan", "--help"}, []string{"-o plan.json"}},
		{[]string{"report", "--help"}, []string{"--format text -o report.txt"}},
		{[]string{"help", "discover"}, []string{"clusterpilot discover -o inventory.json"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := Execute(tc.args, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			got := stdout.String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("help missing %q\n%s", want, got)
				}
			}
		})
	}
}

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
