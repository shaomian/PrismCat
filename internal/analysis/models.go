package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
)

// Redirects must never forward native API key headers to another destination.
var analysisClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

const anthropicVersion = "2023-06-01"

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ModelList struct {
	Models    []Model `json:"models"`
	Truncated bool    `json:"truncated"`
}

func endpointURL(baseURL, resource string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return ""
	}
	path := strings.TrimRight(u.Path, "/")
	complete := false
	for _, suffix := range []string{"/chat/completions", "/messages", "/models"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			complete = true
			break
		}
	}
	if !complete && !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	u.Path = path + "/" + resource
	u.RawPath = ""
	return u.String()
}

func ModelsURL(baseURL string) string { return endpointURL(baseURL, "models") }

// Saved credentials can be reused only for the same API destination, including
// a PrismCat route prefix. Typing another URL must not send the old key there.
func SameModelDestination(first, second string) bool {
	a, b := ModelsURL(first), ModelsURL(second)
	return a != "" && a == b
}

func setAnalysisHeaders(req *http.Request, cfg config.AnalysisConfig) {
	if cfg.Protocol == "anthropic" {
		req.Header.Set("anthropic-version", anthropicVersion)
		if cfg.APIKey != "" {
			req.Header.Set("x-api-key", cfg.APIKey)
		}
	} else if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	req.Header.Set("X-PrismCat-Tag", "prismcat-analysis")
}

// ListModels runs only on an explicit user request. Pagination, response size,
// count and time are bounded; catalogs are not persisted or polled.
func ListModels(ctx context.Context, cfg config.AnalysisConfig) (ModelList, error) {
	out := ModelList{Models: []Model{}}
	var err error
	cfg.Protocol, err = config.NormalizeAnalysisProtocol(cfg.Protocol)
	if err != nil {
		return out, errors.New("analysis_invalid_protocol")
	}
	endpoint := ModelsURL(cfg.BaseURL)
	if endpoint == "" {
		return out, errors.New("analysis_models_invalid_url")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	seen := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	for page := 0; page < 5; page++ {
		u, _ := url.Parse(endpoint)
		if cfg.Protocol == "anthropic" {
			q := u.Query()
			q.Set("limit", "1000")
			if cursor != "" {
				q.Set("after_id", cursor)
			}
			u.RawQuery = q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return out, errors.New("analysis_models_invalid_url")
		}
		setAnalysisHeaders(req, cfg)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		resp, err := analysisClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return out, errors.New("analysis_models_timeout")
			}
			return out, errors.New("analysis_models_failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		resp.Body.Close()
		if readErr != nil && ctx.Err() != nil {
			return out, errors.New("analysis_models_timeout")
		}
		if resp.StatusCode != http.StatusOK {
			switch resp.StatusCode {
			case 401, 403:
				return out, errors.New("analysis_models_unauthorized")
			case 404, 405:
				return out, errors.New("analysis_models_unsupported")
			default:
				return out, errors.New("analysis_models_failed")
			}
		}
		if readErr != nil || len(body) > 2<<20 {
			return out, errors.New("analysis_models_invalid_response")
		}
		var data struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.Unmarshal(body, &data); err != nil || data.Data == nil {
			return out, errors.New("analysis_models_invalid_response")
		}
		for _, m := range data.Data {
			id := strings.TrimSpace(m.ID)
			if id == "" || len(id) > 300 || seen[id] {
				continue
			}
			if len(out.Models) >= 2000 {
				out.Truncated = true
				break
			}
			name := strings.TrimSpace(m.DisplayName)
			if name == "" || len(name) > 300 {
				name = id
			}
			out.Models = append(out.Models, Model{ID: id, Name: name})
			seen[id] = true
		}
		if cfg.Protocol != "anthropic" || !data.HasMore || out.Truncated {
			break
		}
		cursor = data.LastID
		if cursor == "" && len(data.Data) > 0 {
			cursor = data.Data[len(data.Data)-1].ID
		}
		if cursor == "" || cursors[cursor] || page == 4 {
			out.Truncated = true
			break
		}
		cursors[cursor] = true
	}
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].ID < out.Models[j].ID })
	return out, nil
}
