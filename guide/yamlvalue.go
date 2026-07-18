package guide

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// LintYAML validates that body is well-formed YAML — every document parses with
// no trailing content, tolerating {{token}} placeholders by treating each as a
// scalar named after the token (a real substitution runs at compose time; the
// name keeps distinct token keys distinct under yaml.v3's duplicate-key check).
func LintYAML(body []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(neutralizeBareName(body)))
	for {
		var v any
		switch err := dec.Decode(&v); {
		case err == nil:
			continue
		case errors.Is(err, io.EOF):
			return nil
		default:
			return fmt.Errorf("%w: %w", ErrYAMLParse, err)
		}
	}
}
