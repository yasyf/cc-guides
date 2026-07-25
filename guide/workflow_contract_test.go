package guide_test

import (
	"os"
	"strings"
	"testing"
)

func TestGuidesWorkflowRequiresExactCommit(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/guides.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, required := range []string{
		"workflow_call:\n    inputs:\n      commit:",
		"workflow_dispatch:\n    inputs:\n      commit:",
		"description: Exact commit to render and verify\n        required: true",
		"ref: ${{ inputs.commit || github.sha }}",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("guides workflow missing %q", required)
		}
	}
	if got := strings.Count(workflow, "uses: actions/checkout@v7"); got != 2 {
		t.Fatalf("checkout steps = %d, want 2", got)
	}
	if got := strings.Count(workflow, "ref: ${{ inputs.commit || github.sha }}"); got != 2 {
		t.Fatalf("exact checkout refs = %d, want 2", got)
	}
}
