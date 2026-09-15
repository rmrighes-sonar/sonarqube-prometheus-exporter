package main

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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
