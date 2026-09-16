package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSonarTime(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantOK  bool
		wantUTC string // RFC3339 in UTC, only checked when wantOK
	}{
		{"empty", "", false, ""},
		{"invalid", "not-a-time", false, ""},
		// SonarQube's timezone offset has no colon (+0300, not +03:00) --
		// the exact format that trips up naive ISO-8601 parsers.
		{"valid with offset", "2017-03-01T11:39:03+0300", true, "2017-03-01T08:39:03Z"},
		{"valid UTC", "2026-09-15T20:37:57+0000", true, "2026-09-15T20:37:57Z"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSonarTime(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseSonarTime(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if tc.wantOK && got.UTC().Format("2006-01-02T15:04:05Z") != tc.wantUTC {
				t.Errorf("parseSonarTime(%q) = %v, want %v", tc.in, got.UTC(), tc.wantUTC)
			}
		})
	}
}

func TestMeasureEffectiveValue(t *testing.T) {
	overall := Measure{Component: "p1", Metric: "bugs", Value: "5"}
	if got := overall.EffectiveValue(); got != "5" {
		t.Errorf("overall measure EffectiveValue() = %q, want %q", got, "5")
	}

	newCode := Measure{
		Component: "p1",
		Metric:    "new_bugs",
		Value:     "", // SonarQube leaves Value empty for new-code metrics
		Period: &struct {
			Value string `json:"value"`
		}{Value: "2"},
	}
	if got := newCode.EffectiveValue(); got != "2" {
		t.Errorf("new-code measure EffectiveValue() = %q, want %q", got, "2")
	}
}

func TestSonarQubeClientSearchProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-token")
		}
		_ = json.NewEncoder(w).Encode(projectsSearchResponse{
			Components: []Project{{Key: "p1", Name: "Project One", Qualifier: "TRK"}},
		})
	}))
	defer srv.Close()

	client := NewSonarQubeClient(srv.URL, "test-token")
	projects, err := client.SearchProjects()
	if err != nil {
		t.Fatalf("SearchProjects() error = %v", err)
	}
	if len(projects) != 1 || projects[0].Key != "p1" {
		t.Errorf("SearchProjects() = %+v, want one project with key %q", projects, "p1")
	}
}

func TestSonarQubeClientGetNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewSonarQubeClient(srv.URL, "bad-token")
	if _, err := client.SearchProjects(); err == nil {
		t.Fatal("expected an error for a non-200 response, got nil")
	}
}

func TestSonarQubeClientSearchMeasuresEmptyInput(t *testing.T) {
	client := NewSonarQubeClient("http://unused.invalid", "")
	measures, err := client.SearchMeasures(nil, []string{"bugs"})
	if err != nil {
		t.Fatalf("SearchMeasures() error = %v", err)
	}
	if measures != nil {
		t.Errorf("SearchMeasures() with no component keys = %+v, want nil", measures)
	}
}

func TestSonarQubeClientSearchMeasures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/measures/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("projectKeys") != "p1,p2" {
			t.Errorf("projectKeys = %q, want %q", q.Get("projectKeys"), "p1,p2")
		}
		if q.Get("metricKeys") != "bugs,coverage" {
			t.Errorf("metricKeys = %q, want %q", q.Get("metricKeys"), "bugs,coverage")
		}
		_ = json.NewEncoder(w).Encode(measuresSearchResponse{
			Measures: []Measure{
				{Component: "p1", Metric: "bugs", Value: "1"},
				{Component: "p2", Metric: "bugs", Value: "0"},
			},
		})
	}))
	defer srv.Close()

	client := NewSonarQubeClient(srv.URL, "")
	measures, err := client.SearchMeasures([]string{"p1", "p2"}, []string{"bugs", "coverage"})
	if err != nil {
		t.Fatalf("SearchMeasures() error = %v", err)
	}
	if len(measures) != 2 {
		t.Fatalf("SearchMeasures() returned %d measures, want 2", len(measures))
	}
}

func TestSonarQubeClientSearchPortfolios(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/components/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("qualifiers"); got != "VW" {
			t.Errorf("qualifiers = %q, want %q", got, "VW")
		}
		_ = json.NewEncoder(w).Encode(componentsSearchResponse{
			Components: []Portfolio{{Key: "port1", Name: "Portfolio One"}},
		})
	}))
	defer srv.Close()

	client := NewSonarQubeClient(srv.URL, "")
	portfolios, err := client.SearchPortfolios()
	if err != nil {
		t.Fatalf("SearchPortfolios() error = %v", err)
	}
	if len(portfolios) != 1 || portfolios[0].Key != "port1" {
		t.Errorf("SearchPortfolios() = %+v, want one portfolio with key %q", portfolios, "port1")
	}
}

func TestSonarQubeClientSearchPortfoliosEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(componentsSearchResponse{})
	}))
	defer srv.Close()

	client := NewSonarQubeClient(srv.URL, "")
	portfolios, err := client.SearchPortfolios()
	if err != nil {
		t.Fatalf("SearchPortfolios() error = %v", err)
	}
	if len(portfolios) != 0 {
		t.Errorf("SearchPortfolios() = %+v, want empty (non-Enterprise edition)", portfolios)
	}
}

func TestSonarQubeClientRecentAnalyses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ce/activity" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("type") != "REPORT" {
			t.Errorf("type = %q, want %q", q.Get("type"), "REPORT")
		}
		_ = json.NewEncoder(w).Encode(ceActivityResponse{
			Tasks: []CETask{
				{ComponentKey: "p1", Status: "SUCCESS", SubmittedAt: "2026-09-15T20:00:00+0000"},
			},
		})
	}))
	defer srv.Close()

	client := NewSonarQubeClient(srv.URL, "")
	tasks, err := client.RecentAnalyses()
	if err != nil {
		t.Fatalf("RecentAnalyses() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ComponentKey != "p1" || tasks[0].Status != "SUCCESS" {
		t.Errorf("RecentAnalyses() = %+v, want one SUCCESS task for p1", tasks)
	}
}

func TestSonarQubeClientGetRequestBuildError(t *testing.T) {
	// A control character in the host makes url.Parse (inside
	// http.NewRequest) fail, exercising the "building request" error path
	// without needing a live server.
	client := NewSonarQubeClient("http://example.com/\x7f", "")
	if _, err := client.SearchProjects(); err == nil {
		t.Fatal("expected an error building the request, got nil")
	}
}
