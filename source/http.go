package source

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

// httpClient bounds the codeload download; a fragment tree is tiny, so a short
// timeout is generous.
var httpClient = &http.Client{Timeout: 60 * time.Second}

// newHTTPRequest builds a codeload GET, adding a Bearer token only when
// GITHUB_TOKEN is set. Public fragment repos need no auth, so an unset token is
// the normal case and must not send an empty Authorization header.
func newHTTPRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetch, err)
	}
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}
