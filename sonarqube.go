package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SonarQubeClient is a minimal client for the subset of SonarQube's Web API
// this exporter needs. It intentionally does not try to be a general-purpose
// SDK.
type SonarQubeClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewSonarQubeClient builds a client targeting baseURL, authenticating with a
// bearer token. An empty token results in unauthenticated requests, which
// will fail against any project/portfolio that isn't public.
func NewSonarQubeClient(baseURL, token string) *SonarQubeClient {
	return &SonarQubeClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *SonarQubeClient) get(path string, query url.Values, out interface{}) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", path, err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("requesting %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %s", path, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding response from %s: %w", path, err)
	}
	return nil
}

// Project is one entry from GET /api/projects/search.
type Project struct {
	Key              string `json:"key"`
	Name             string `json:"name"`
	Qualifier        string `json:"qualifier"`
	Visibility       string `json:"visibility"`
	LastAnalysisDate string `json:"lastAnalysisDate"`
	Revision         string `json:"revision"`
}

type projectsSearchResponse struct {
	Components []Project `json:"components"`
}

// SearchProjects discovers every project (qualifier TRK) the exporter's
// token has Browse permission on. Unlike sonarqube-compose's dashboard-level
// Grafana variables, this list is always live -- no manual key list to keep
// in sync.
func (c *SonarQubeClient) SearchProjects() ([]Project, error) {
	var out projectsSearchResponse
	err := c.get("/api/projects/search", url.Values{"ps": {"500"}}, &out)
	return out.Components, err
}

// Portfolio is one entry from GET /api/components/search?qualifiers=VW.
type Portfolio struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type componentsSearchResponse struct {
	Components []Portfolio `json:"components"`
}

// SearchPortfolios discovers every portfolio (qualifier VW). Requires
// Governance/Enterprise; returns an empty (not erroring) list otherwise.
func (c *SonarQubeClient) SearchPortfolios() ([]Portfolio, error) {
	var out componentsSearchResponse
	err := c.get("/api/components/search", url.Values{"qualifiers": {"VW"}, "ps": {"500"}}, &out)
	return out.Components, err
}

// Measure is one entry from GET /api/measures/search. For new-code metrics
// (new_bugs, new_vulnerabilities, new_coverage), SonarQube nests the actual
// value under Period instead of Value.
type Measure struct {
	Component string `json:"component"`
	Metric    string `json:"metric"`
	Value     string `json:"value"`
	Period    *struct {
		Value string `json:"value"`
	} `json:"period,omitempty"`
}

type measuresSearchResponse struct {
	Measures []Measure `json:"measures"`
}

// EffectiveValue returns Period.Value when present (new-code metrics),
// otherwise Value (overall-code metrics).
func (m Measure) EffectiveValue() string {
	if m.Period != nil {
		return m.Period.Value
	}
	return m.Value
}

// SearchMeasures fetches metricKeys for componentKeys in a single call.
// /api/measures/search is internal/undocumented (same caveat as the Infinity
// queries it replaces) -- if it stops working on a future SonarQube upgrade,
// /api/measures/component (one call per component) is the stable fallback.
func (c *SonarQubeClient) SearchMeasures(componentKeys, metricKeys []string) ([]Measure, error) {
	if len(componentKeys) == 0 {
		return nil, nil
	}
	var out measuresSearchResponse
	err := c.get("/api/measures/search", url.Values{
		"projectKeys": {strings.Join(componentKeys, ",")},
		"metricKeys":  {strings.Join(metricKeys, ",")},
	}, &out)
	return out.Measures, err
}

// CETask is one entry from GET /api/ce/activity.
type CETask struct {
	ComponentKey string `json:"componentKey"`
	Status       string `json:"status"`
	SubmittedAt  string `json:"submittedAt"`
}

type ceActivityResponse struct {
	Tasks []CETask `json:"tasks"`
}

// RecentAnalyses fetches the most recent Compute Engine REPORT tasks, used
// to derive a per-project "last analysis status" gauge. The full event list
// (exact timestamps, durations, error messages) intentionally isn't
// re-exposed as metrics -- see the README's trade-offs section.
func (c *SonarQubeClient) RecentAnalyses() ([]CETask, error) {
	var out ceActivityResponse
	err := c.get("/api/ce/activity", url.Values{
		"ps":     {"500"},
		"type":   {"REPORT"},
		"status": {"SUCCESS,FAILED,CANCELED"},
	}, &out)
	return out.Tasks, err
}

// parseSonarTime parses SonarQube's timestamp format, e.g.
// "2017-03-01T11:39:03+0300" -- note the timezone offset has no colon, which
// trips up naive ISO-8601 parsers (this is the exact issue that previously
// broke Grafana's Infinity-datasource timestamp columns; Go's time package
// handles it natively with the right layout).
func parseSonarTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02T15:04:05Z0700", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
