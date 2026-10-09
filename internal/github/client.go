package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	// BaseURL is the REST API root, without a trailing slash.
	BaseURL string
	token   string
	http    *http.Client
	login   string // the token owner, cached by viewer
}

// viewer returns the login of the token owner.
func (g *Client) viewer(ctx context.Context) (string, error) {
	if g.login == "" {
		var u ghUser
		if err := g.get(ctx, g.BaseURL+"/user", &u); err != nil {
			return "", err
		}
		g.login = u.Login
	}
	return g.login, nil
}

func New(token string) *Client {
	return &Client{BaseURL: "https://api.github.com", token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

func (g *Client) do(ctx context.Context, method, rawURL string, header http.Header, out any) (*http.Response, error) {
	return g.send(ctx, method, rawURL, header, nil, out)
}

func (g *Client) send(ctx context.Context, method, rawURL string, header http.Header, body io.Reader, out any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusNotModified:
		return res, nil
	case res.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return res, fmt.Errorf("%s %s: %s: %s", method, rawURL, res.Status, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return res, fmt.Errorf("%s %s: decode: %w", method, rawURL, err)
		}
	}
	return res, nil
}

func (g *Client) get(ctx context.Context, rawURL string, out any) error {
	_, err := g.do(ctx, http.MethodGet, rawURL, nil, out)
	return err
}

// graphql runs a GraphQL query and decodes its data into out.
func (g *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	var res struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err := g.send(ctx, http.MethodPost, g.BaseURL+"/graphql", nil, bytes.NewReader(body), &res); err != nil {
		return err
	}
	if len(res.Errors) > 0 {
		return fmt.Errorf("graphql: %s", res.Errors[0].Message)
	}
	return json.Unmarshal(res.Data, out)
}

// linkRel extracts the URL for rel from an RFC 8288 Link header.
func linkRel(link, rel string) string {
	for part := range strings.SplitSeq(link, ",") {
		u, params, ok := strings.Cut(part, ";")
		if ok && strings.Contains(params, `rel="`+rel+`"`) {
			return strings.Trim(strings.TrimSpace(u), "<>")
		}
	}
	return ""
}

// htmlURL converts an API URL to its github.com page as a fallback when the API object can't be fetched.
func htmlURL(apiURL string) string {
	path, ok := strings.CutPrefix(apiURL, "https://api.github.com/repos/")
	if !ok {
		return apiURL
	}
	// owner/repo/kind/rest; the page path uses the singular kind for PRs and commits.
	parts := strings.SplitN(path, "/", 4)
	if len(parts) == 4 {
		switch parts[2] {
		case "pulls":
			parts[2] = "pull"
		case "commits":
			parts[2] = "commit"
		}
	}
	return "https://github.com/" + strings.Join(parts, "/")
}
