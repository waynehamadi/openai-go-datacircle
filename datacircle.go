// Package datacircle gives the OpenAI Go SDK (github.com/openai/openai-go/v3) four function tools on Datacircle's open API: find the best GTM APIs.
//
// The API needs no key: https://api.datacircle.dev/v1 (its spec: https://api.datacircle.dev/v1/openapi.json).
// More at https://datacircle.dev
package datacircle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// BaseURL is the API's address.
const BaseURL = "https://api.datacircle.dev/v1"

// Version is this module's version, sent in each call's User-Agent.
const Version = "0.1.0"

// Config sets where the tools call. Its zero value, or nil, calls BaseURL with http.DefaultClient.
type Config struct {
	BaseURL    string
	HTTPClient *http.Client
}

// apiError is an answer of 4xx: the API's own words, given back as the tool's result.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return e.message }

type client struct {
	base string
	http *http.Client
}

func newClient(cfg *Config) *client {
	c := &client{base: BaseURL, http: http.DefaultClient}
	if cfg != nil && cfg.BaseURL != "" {
		c.base = strings.TrimRight(cfg.BaseURL, "/")
	}
	if cfg != nil && cfg.HTTPClient != nil {
		c.http = cfg.HTTPClient
	}
	return c
}

// get calls one path and decodes its JSON into out. A 429 is waited for once, a minute at most.
func (c *client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	for try := 0; ; try++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "openai-go-datacircle/"+Version)
		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("datacircle: %w", err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return fmt.Errorf("datacircle: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && try == 0 {
			wait, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			if wait < 1 || wait > 60 {
				wait = 60
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(wait) * time.Second):
			}
			continue
		}
		if res.StatusCode >= 400 && res.StatusCode < 500 {
			var e struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(body, &e) != nil || e.Error == "" {
				e.Error = res.Status
			}
			return &apiError{status: res.StatusCode, message: e.Error}
		}
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("datacircle: %s", res.Status)
		}
		return json.Unmarshal(body, out)
	}
}

// planKeys: a plan holds about 90 keys (its terms of use, its aggregators...); these choose a plan, the rest is on its
// page: 10 plans of these keys take about 2,500 bytes of a model's context.
var planKeys = []string{"api", "rank", "provider", "plan", "score", "perThousand", "notFoundPerThousand", "billing", "pay",
	"currency", "page"}

func brief(item map[string]any, keys []string) map[string]any {
	kept := map[string]any{}
	for _, k := range keys {
		if v, ok := item[k]; ok && v != nil {
			kept[k] = v
		}
	}
	return kept
}

func briefPlans(plans []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(plans))
	for _, p := range plans {
		out = append(out, brief(p, planKeys))
	}
	return out
}

// slugify is the API's rule for a slug: the name in lowercase letters and digits, the rest a hyphen.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFKD.String(strings.ToLower(strings.TrimSpace(s))) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	return b.String()
}

func clamp(n, low, high int) int {
	if n < low {
		return low
	}
	if n > high {
		return high
	}
	return n
}
