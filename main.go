// Command sonarqube-prometheus-exporter is a Prometheus exporter for SonarQube
// project/portfolio business data (quality gates, issues, coverage, ratings)
// that SonarQube's own native /api/monitoring/metrics endpoint doesn't
// expose. See README.md for the full metric contract and rationale.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// newMux builds the exporter's HTTP handler, factored out of main so it's
// testable without binding a real listener.
func newMux(collector *Collector) *http.ServeMux {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>sonarqube-prometheus-exporter</h1><p><a href="/metrics">/metrics</a></p></body></html>`))
	})
	// Liveness endpoint for orchestrators (e.g. this repo's own Docker
	// Compose healthcheck) -- deliberately doesn't call out to SonarQube:
	// it only confirms the exporter process itself is up and serving, so
	// SonarQube being temporarily unreachable (reflected instead via the
	// sonarqube_prometheus_exporter_up gauge on /metrics) doesn't get the container
	// killed/restarted by its own orchestrator.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return mux
}

func main() {
	sonarqubeURL := getenv("SONARQUBE_URL", "http://sonarqube:9000")
	token := os.Getenv("SONARQUBE_API_TOKEN")
	listenAddr := getenv("LISTEN_ADDR", ":9091")

	if token == "" {
		log.Println("sonarqube-prometheus-exporter: warning: SONARQUBE_API_TOKEN is not set; requests to SonarQube will be unauthenticated and will likely fail for any non-public project")
	}

	client := NewSonarQubeClient(sonarqubeURL, token)
	collector := NewCollector(client)
	mux := newMux(collector)

	log.Printf("sonarqube-prometheus-exporter: listening on %s, target SonarQube at %s", listenAddr, sonarqubeURL)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
}
