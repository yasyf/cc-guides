package guide_test

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
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
		"ref: ${{ inputs.commit || github.event.pull_request.head.sha }}",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("guides workflow missing %q", required)
		}
	}
	if got := strings.Count(workflow, "uses: actions/checkout@v7"); got != 2 {
		t.Fatalf("checkout steps = %d, want 2", got)
	}
	if got := strings.Count(workflow, "ref: ${{ inputs.commit || github.sha }}"); got != 1 {
		t.Fatalf("exact render checkout refs = %d, want 1", got)
	}
	if got := strings.Count(workflow, "ref: ${{ inputs.commit || github.event.pull_request.head.sha }}"); got != 1 {
		t.Fatalf("exact PR checkout refs = %d, want 1", got)
	}
}

func TestGuidesWorkflowScopesExplicitCommitConcurrency(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/guides.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On struct {
			WorkflowCall struct {
				Inputs map[string]struct {
					Type     string
					Default  bool
					Required bool
				}
			} `yaml:"workflow_call"`
		} `yaml:"on"`
		Jobs map[string]struct {
			Concurrency struct {
				Group            string
				CancelInProgress bool `yaml:"cancel-in-progress"`
			}
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	parallel := workflow.On.WorkflowCall.Inputs["parallel_commits"]
	if parallel.Type != "boolean" || parallel.Default || parallel.Required {
		t.Fatalf("parallel_commits schema = %+v, want optional boolean default false", parallel)
	}
	render := workflow.Jobs["render"].Concurrency
	want := "cc-guides-render${{ (github.event_name == 'workflow_dispatch' || inputs.parallel_commits) && format('-{0}', inputs.commit || github.sha) || '' }}"
	if render.Group != want {
		t.Fatalf("render concurrency group = %q, want %q", render.Group, want)
	}
	if render.CancelInProgress {
		t.Fatal("render cancels an in-progress writer")
	}
}
