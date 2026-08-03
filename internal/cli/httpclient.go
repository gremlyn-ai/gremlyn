package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// apiGet performs a GET request to the given URL and returns the response body.
func apiGet(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// shieldURL returns the Shield API base URL from env or default.
func shieldURL() string {
	if u := os.Getenv("GREMLYN_SHIELD_URL"); u != "" {
		return u
	}
	return "http://localhost:8081"
}

// arenaURL returns the Arena API base URL from env or default.
func arenaURL() string {
	if u := os.Getenv("GREMLYN_ARENA_URL"); u != "" {
		return u
	}
	return "http://localhost:8082"
}
