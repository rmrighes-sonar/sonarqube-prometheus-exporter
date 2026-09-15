// Command sonarqube-exporter is a Prometheus exporter for SonarQube
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

func main() {
	sonarqubeURL := getenv("SONARQUBE_URL", "http://sonarqube:9000")
	token := os.Getenv("SONARQUBE_API_TOKEN")
	listenAddr := getenv("LISTEN_ADDR", ":9091")

	if token == "" {
		log.Println("sonarqube-exporter: warning: SONARQUBE_API_TOKEN is not set; requests to SonarQube will be unauthenticated and will likely fail for any non-public project")
	}

	client := NewSonarQubeClient(sonarqubeURL, token)
	collector := NewCollector(client)

	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>sonarqube-exporter</h1><p><a href="/metrics">/metrics</a></p></body></html>`))
	})

	log.Printf("sonarqube-exporter: listening on %s, target SonarQube at %s", listenAddr, sonarqubeURL)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
}
