package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestPivotMeasures(t *testing.T) {
	overall := []Measure{
		{Component: "p1", Metric: "bugs", Value: "3"},
		{Component: "p1", Metric: "coverage", Value: "72.5"},
		{Component: "p2", Metric: "bugs", Value: "0"},
	}
	newCode := []Measure{
		{Component: "p1", Metric: "new_bugs", Period: &struct {
			Value string `json:"value"`
		}{Value: "1"}},
	}

	got := pivotMeasures(overall, newCode)

	if got["p1"]["bugs"] != "3" {
		t.Errorf("p1 bugs = %q, want %q", got["p1"]["bugs"], "3")
	}
	if got["p1"]["coverage"] != "72.5" {
		t.Errorf("p1 coverage = %q, want %q", got["p1"]["coverage"], "72.5")
	}
	if got["p1"]["new_bugs"] != "1" {
		t.Errorf("p1 new_bugs = %q, want %q", got["p1"]["new_bugs"], "1")
	}
	if got["p2"]["bugs"] != "0" {
		t.Errorf("p2 bugs = %q, want %q", got["p2"]["bugs"], "0")
	}
}

func TestPivotMeasuresNilNewCode(t *testing.T) {
	overall := []Measure{{Component: "p1", Metric: "bugs", Value: "3"}}

	got := pivotMeasures(overall, nil)

	if got["p1"]["bugs"] != "3" {
		t.Errorf("p1 bugs = %q, want %q", got["p1"]["bugs"], "3")
	}
}

func TestEmitGauge(t *testing.T) {
	desc := prometheus.NewDesc("test_gauge", "help", []string{"project"}, nil)

	t.Run("valid value", func(t *testing.T) {
		ch := make(chan prometheus.Metric, 1)
		emitGauge(ch, desc, "42.5", "p1")
		close(ch)

		metrics := drain(ch)
		if len(metrics) != 1 {
			t.Fatalf("got %d metrics, want 1", len(metrics))
		}
	})

	t.Run("empty value is skipped", func(t *testing.T) {
		ch := make(chan prometheus.Metric, 1)
		emitGauge(ch, desc, "", "p1")
		close(ch)

		if metrics := drain(ch); len(metrics) != 0 {
			t.Fatalf("got %d metrics for an empty value, want 0", len(metrics))
		}
	})

	t.Run("unparseable value is skipped", func(t *testing.T) {
		ch := make(chan prometheus.Metric, 1)
		emitGauge(ch, desc, "not-a-number", "p1")
		close(ch)

		if metrics := drain(ch); len(metrics) != 0 {
			t.Fatalf("got %d metrics for an unparseable value, want 0", len(metrics))
		}
	})
}

func TestEmitStatusMetrics(t *testing.T) {
	desc := prometheus.NewDesc("test_status", "help", []string{"project", "status"}, nil)
	statuses := []string{"OK", "ERROR", "WARN", "NONE"}

	t.Run("emits one metric per status", func(t *testing.T) {
		ch := make(chan prometheus.Metric, len(statuses))
		emitStatusMetrics(ch, desc, "OK", statuses, "p1")
		close(ch)

		metrics := drain(ch)
		if len(metrics) != len(statuses) {
			t.Fatalf("got %d metrics, want %d (one per possible status)", len(metrics), len(statuses))
		}
	})

	t.Run("empty current status emits nothing", func(t *testing.T) {
		ch := make(chan prometheus.Metric, len(statuses))
		emitStatusMetrics(ch, desc, "", statuses, "p1")
		close(ch)

		if metrics := drain(ch); len(metrics) != 0 {
			t.Fatalf("got %d metrics for an empty status, want 0", len(metrics))
		}
	})
}

func drain(ch <-chan prometheus.Metric) []prometheus.Metric {
	var out []prometheus.Metric
	for m := range ch {
		out = append(out, m)
	}
	return out
}

// groupByDesc buckets drained metrics by their *prometheus.Desc identity --
// safe because every call site in collector.go uses one of the package-level
// desc vars, so pointer equality reliably distinguishes metric families.
func groupByDesc(metrics []prometheus.Metric) map[*prometheus.Desc][]prometheus.Metric {
	out := map[*prometheus.Desc][]prometheus.Metric{}
	for _, m := range metrics {
		out[m.Desc()] = append(out[m.Desc()], m)
	}
	return out
}

func metricValue(t *testing.T, m prometheus.Metric) (float64, []string) {
	t.Helper()
	var pb dto.Metric
	if err := m.Write(&pb); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	labels := make([]string, len(pb.Label))
	for i, l := range pb.Label {
		labels[i] = l.GetValue()
	}
	switch {
	case pb.Gauge != nil:
		return pb.Gauge.GetValue(), labels
	case pb.Counter != nil:
		return pb.Counter.GetValue(), labels
	default:
		t.Fatalf("metric %v has neither Gauge nor Counter", m.Desc())
		return 0, nil
	}
}

// newFakeSonarQube stands in for a real SonarQube server, serving just
// enough of the Web API for a full Collector.Collect() pass: two projects
// (one with a last-analysis date, one without), one portfolio, overall +
// new-code measures, and one Compute Engine task.
func newFakeSonarQube(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/projects/search", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(projectsSearchResponse{
			Components: []Project{
				{Key: "p1", Name: "Project One", Qualifier: "TRK", Visibility: "public", LastAnalysisDate: "2026-09-15T20:00:00+0000", Revision: "abc123"},
				{Key: "p2", Name: "Project Two", Qualifier: "TRK", Visibility: "private"},
			},
		})
	})

	mux.HandleFunc("/api/components/search", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(componentsSearchResponse{
			Components: []Portfolio{{Key: "port1", Name: "Portfolio One"}},
		})
	})

	mux.HandleFunc("/api/measures/search", func(w http.ResponseWriter, r *http.Request) {
		metricKeys := r.URL.Query().Get("metricKeys")
		if metricKeys == joinKeys(newCodeMetricKeys) {
			_ = json.NewEncoder(w).Encode(measuresSearchResponse{
				Measures: []Measure{
					{Component: "p1", Metric: "new_bugs", Period: &struct {
						Value string `json:"value"`
					}{Value: "1"}},
				},
			})
			return
		}
		// Overall-code (project) and portfolio measures share this branch --
		// harmless to return the same fixed set regardless of which
		// component/metric keys were actually requested for this test.
		_ = json.NewEncoder(w).Encode(measuresSearchResponse{
			Measures: []Measure{
				{Component: "p1", Metric: "alert_status", Value: "OK"},
				{Component: "p1", Metric: "bugs", Value: "2"},
				{Component: "p1", Metric: "coverage", Value: "75.5"},
				{Component: "p1", Metric: "ncloc", Value: "500"},
				{Component: "p2", Metric: "alert_status", Value: "ERROR"},
				{Component: "port1", Metric: "alert_status", Value: "OK"},
				{Component: "port1", Metric: "bugs", Value: "3"},
			},
		})
	})

	mux.HandleFunc("/api/ce/activity", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ceActivityResponse{
			Tasks: []CETask{
				{ComponentKey: "p1", Status: "SUCCESS", SubmittedAt: "2026-09-15T20:00:00+0000"},
			},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func joinKeys(keys []string) string {
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ","
		}
		out += k
	}
	return out
}

func TestNewCollector(t *testing.T) {
	client := NewSonarQubeClient("http://unused.invalid", "token")
	c := NewCollector(client)

	if c.client != client {
		t.Error("NewCollector() did not store the given client")
	}
}

func TestCollectorDescribe(t *testing.T) {
	c := NewCollector(NewSonarQubeClient("http://unused.invalid", ""))

	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	var count int
	for range ch {
		count++
	}
	// Describe intentionally only describes the scrapeErrors counter --
	// per-project/portfolio series are data-dependent and can't be
	// described ahead of time (see the comment on Collector.Describe).
	if count != 1 {
		t.Errorf("Describe() sent %d descriptors, want 1", count)
	}
}

// findMetricByLabel returns the value of the first metric whose label at
// labelIdx equals want (and, if len(want) > 1, whose remaining labels also
// match positionally), and whether a match was found. Flattens what would
// otherwise be a repeated loop+if per assertion in the tests below.
func findMetricByLabel(t *testing.T, metrics []prometheus.Metric, want ...string) (float64, bool) {
	t.Helper()
	for _, m := range metrics {
		v, labels := metricValue(t, m)
		if len(labels) < len(want) {
			continue
		}
		if labelsMatch(labels, want) {
			return v, true
		}
	}
	return 0, false
}

func labelsMatch(labels, want []string) bool {
	for i, w := range want {
		if labels[i] != w {
			return false
		}
	}
	return true
}

func collectFromFakeSonarQube(t *testing.T) map[*prometheus.Desc][]prometheus.Metric {
	t.Helper()
	srv := newFakeSonarQube(t)
	c := NewCollector(NewSonarQubeClient(srv.URL, "test-token"))

	ch := make(chan prometheus.Metric, 200)
	c.Collect(ch)
	close(ch)

	return groupByDesc(drain(ch))
}

func TestCollectorCollectSuccess(t *testing.T) {
	byDesc := collectFromFakeSonarQube(t)

	upMetrics := byDesc[upDesc]
	if len(upMetrics) != 1 {
		t.Fatalf("got %d %v metrics, want 1", len(upMetrics), upDesc)
	}
	if v, _ := metricValue(t, upMetrics[0]); v != 1 {
		t.Errorf("sonarqube_exporter_up = %v, want 1", v)
	}

	// Both projects got a project_info series.
	if got := len(byDesc[projectInfoDesc]); got != 2 {
		t.Errorf("got %d project_info metrics, want 2", got)
	}

	// Only p1 has a last-analysis date.
	if got := len(byDesc[projectLastAnalysisDesc]); got != 1 {
		t.Errorf("got %d project_last_analysis_timestamp_seconds metrics, want 1 (p1 only)", got)
	}

	// The portfolio was collected too (Governance/Enterprise-only feature).
	if got := len(byDesc[portfolioInfoDesc]); got != 1 {
		t.Errorf("got %d portfolio_info metrics, want 1", got)
	}
}

func TestCollectorCollectMeasuresRoundTrip(t *testing.T) {
	byDesc := collectFromFakeSonarQube(t)

	// p1's coverage measure round-tripped correctly.
	if v, ok := findMetricByLabel(t, byDesc[projectCoverageDesc], "p1"); !ok {
		t.Error("no coverage metric found for p1")
	} else if v != 75.5 {
		t.Errorf("p1 coverage = %v, want 75.5", v)
	}

	// p1's new-code bugs (nested under Period) round-tripped correctly.
	if v, ok := findMetricByLabel(t, byDesc[projectNewBugsDesc], "p1"); !ok {
		t.Error("no new_bugs metric found for p1")
	} else if v != 1 {
		t.Errorf("p1 new_bugs = %v, want 1", v)
	}
}

func TestCollectorCollectLastAnalysisStatus(t *testing.T) {
	byDesc := collectFromFakeSonarQube(t)

	// p1's last Compute Engine task was SUCCESS.
	v, ok := findMetricByLabel(t, byDesc[projectLastAnalysisStatusDesc], "p1", "SUCCESS")
	if !ok {
		t.Fatal("expected a p1/SUCCESS last_analysis_status metric, found none")
	}
	if v != 1 {
		t.Errorf("p1 last_analysis_status{status=SUCCESS} = %v, want 1", v)
	}
}

func TestCollectorCollectProjectsSearchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewCollector(NewSonarQubeClient(srv.URL, ""))
	ch := make(chan prometheus.Metric, 50)
	c.Collect(ch)
	close(ch)

	byDesc := groupByDesc(drain(ch))

	up := byDesc[upDesc]
	if len(up) != 1 {
		t.Fatalf("got %d %v metrics, want 1", len(up), upDesc)
	}
	if v, _ := metricValue(t, up[0]); v != 0 {
		t.Errorf("sonarqube_exporter_up = %v, want 0 after a failed scrape", v)
	}
}

func TestCollectorCollectNoProjectsNoPortfolios(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/search":
			_ = json.NewEncoder(w).Encode(projectsSearchResponse{})
		case "/api/components/search":
			_ = json.NewEncoder(w).Encode(componentsSearchResponse{})
		default:
			t.Errorf("unexpected call to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewCollector(NewSonarQubeClient(srv.URL, ""))
	ch := make(chan prometheus.Metric, 20)
	c.Collect(ch)
	close(ch)

	byDesc := groupByDesc(drain(ch))
	if v, _ := metricValue(t, byDesc[upDesc][0]); v != 1 {
		t.Errorf("sonarqube_exporter_up = %v, want 1 (empty results aren't an error)", v)
	}
	if got := len(byDesc[projectInfoDesc]); got != 0 {
		t.Errorf("got %d project_info metrics, want 0", got)
	}
}
