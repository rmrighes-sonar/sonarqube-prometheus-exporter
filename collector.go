package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "sonarqube"

// Metric keys requested per scrape. Mirrors the columns sonarqube-compose's
// Usage dashboard previously queried directly via the Infinity datasource.
var (
	overallMetricKeys = []string{
		"alert_status", "bugs", "vulnerabilities", "security_hotspots",
		"code_smells", "coverage", "duplicated_lines_density", "ncloc",
		"security_rating", "reliability_rating", "sqale_rating",
	}
	newCodeMetricKeys = []string{"new_bugs", "new_vulnerabilities", "new_coverage"}

	portfolioMetricKeys = []string{
		"alert_status", "bugs", "vulnerabilities", "ncloc",
		"coverage", "duplicated_lines_density", "security_rating", "reliability_rating",
	}

	qualityGateStatuses  = []string{"OK", "ERROR", "WARN", "NONE"}
	analysisTaskStatuses = []string{"SUCCESS", "FAILED", "CANCELED"}
)

func desc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(namespace+"_"+name, help, labels, nil)
}

var (
	projectInfoDesc               = desc("project_info", "Static project attributes (always 1); join on project for name/qualifier/visibility/revision.", "project", "name", "qualifier", "visibility", "revision")
	projectLastAnalysisDesc       = desc("project_last_analysis_timestamp_seconds", "Unix timestamp of the project's last analysis.", "project")
	projectQualityGateDesc        = desc("project_quality_gate_status", "1 if the project's quality gate currently has this status, 0 otherwise.", "project", "status")
	projectBugsDesc               = desc("project_bugs", "Number of bugs on overall code.", "project")
	projectNewBugsDesc            = desc("project_new_bugs", "Number of bugs on new code.", "project")
	projectVulnerabilitiesDesc    = desc("project_vulnerabilities", "Number of vulnerabilities on overall code.", "project")
	projectNewVulnerabilitiesDesc = desc("project_new_vulnerabilities", "Number of vulnerabilities on new code.", "project")
	projectHotspotsDesc           = desc("project_security_hotspots", "Number of security hotspots on overall code.", "project")
	projectCodeSmellsDesc         = desc("project_code_smells", "Number of code smells on overall code.", "project")
	projectCoverageDesc           = desc("project_coverage_percent", "Overall code test coverage, percent.", "project")
	projectNewCoverageDesc        = desc("project_new_coverage_percent", "New code test coverage, percent.", "project")
	projectDuplicationDesc        = desc("project_duplicated_lines_percent", "Percentage of duplicated lines on overall code.", "project")
	projectLoCDesc                = desc("project_lines_of_code", "Lines of code (ncloc).", "project")
	projectReliabilityDesc        = desc("project_reliability_rating", "Reliability rating, 1 (A) to 5 (E).", "project")
	projectSecurityDesc           = desc("project_security_rating", "Security rating, 1 (A) to 5 (E).", "project")
	projectMaintainabilityDesc    = desc("project_maintainability_rating", "Maintainability rating, 1 (A) to 5 (E).", "project")
	projectLastAnalysisStatusDesc = desc("project_last_analysis_status", "1 if the project's most recent Compute Engine analysis currently has this status, 0 otherwise.", "project", "status")

	portfolioInfoDesc            = desc("portfolio_info", "Static portfolio attributes (always 1); join on portfolio for name.", "portfolio", "name")
	portfolioQualityGateDesc     = desc("portfolio_quality_gate_status", "1 if the portfolio's quality gate currently has this status, 0 otherwise.", "portfolio", "status")
	portfolioBugsDesc            = desc("portfolio_bugs", "Number of bugs on overall code.", "portfolio")
	portfolioVulnerabilitiesDesc = desc("portfolio_vulnerabilities", "Number of vulnerabilities on overall code.", "portfolio")
	portfolioCoverageDesc        = desc("portfolio_coverage_percent", "Overall code test coverage, percent.", "portfolio")
	portfolioDuplicationDesc     = desc("portfolio_duplicated_lines_percent", "Percentage of duplicated lines on overall code.", "portfolio")
	portfolioLoCDesc             = desc("portfolio_lines_of_code", "Lines of code (ncloc).", "portfolio")
	portfolioReliabilityDesc     = desc("portfolio_reliability_rating", "Reliability rating, 1 (A) to 5 (E).", "portfolio")
	portfolioSecurityDesc        = desc("portfolio_security_rating", "Security rating, 1 (A) to 5 (E).", "portfolio")

	upDesc             = desc("prometheus_exporter_up", "Whether the last scrape of SonarQube's Web API succeeded (1) or not (0).")
	scrapeDurationDesc = desc("prometheus_exporter_scrape_duration_seconds", "Duration of the last scrape of SonarQube's Web API, in seconds.")
)

// Collector implements prometheus.Collector by querying SonarQube's Web API
// fresh on every scrape (the same idiom mysqld_exporter/postgres_exporter
// use), rather than caching in a background poller. Metrics are emitted via
// MustNewConstMetric -- stateless per scrape -- so a project/portfolio that's
// deleted from SonarQube simply stops appearing, instead of leaving a stale
// series behind the way a persistently registered GaugeVec would.
type Collector struct {
	client       *SonarQubeClient
	scrapeErrors prometheus.Counter
}

// NewCollector builds a Collector querying SonarQube through client.
func NewCollector(client *SonarQubeClient) *Collector {
	return &Collector{
		client: client,
		scrapeErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "prometheus_exporter_scrape_errors_total",
			Help:      "Total number of errors encountered while scraping SonarQube's Web API.",
		}),
	}
}

// Describe intentionally sends nothing: per-project/portfolio series use a
// dynamic, data-dependent label set (which projects exist right now), so
// they can't be described ahead of time. This makes the collector
// "unchecked", which is safe as long as Collect never panics -- every
// MustNewConstMetric call below uses a fixed, hand-verified label count.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	c.scrapeErrors.Describe(ch)
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	up := 1.0

	if err := c.collectProjects(ch); err != nil {
		log.Printf("sonarqube-prometheus-exporter: error collecting projects: %v", err)
		up = 0
		c.scrapeErrors.Inc()
	}
	if err := c.collectPortfolios(ch); err != nil {
		log.Printf("sonarqube-prometheus-exporter: error collecting portfolios: %v", err)
		up = 0
		c.scrapeErrors.Inc()
	}

	ch <- prometheus.MustNewConstMetric(upDesc, prometheus.GaugeValue, up)
	ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds())
	c.scrapeErrors.Collect(ch)
}

func (c *Collector) collectProjects(ch chan<- prometheus.Metric) error {
	projects, err := c.client.SearchProjects()
	if err != nil {
		return fmt.Errorf("projects/search: %w", err)
	}
	if len(projects) == 0 {
		return nil
	}

	keys := make([]string, len(projects))
	for i, p := range projects {
		keys[i] = p.Key
	}

	overall, err := c.client.SearchMeasures(keys, overallMetricKeys)
	if err != nil {
		return fmt.Errorf("measures/search (overall): %w", err)
	}
	newCode, err := c.client.SearchMeasures(keys, newCodeMetricKeys)
	if err != nil {
		return fmt.Errorf("measures/search (new code): %w", err)
	}

	values := pivotMeasures(overall, newCode)

	for _, p := range projects {
		mv := values[p.Key]

		ch <- prometheus.MustNewConstMetric(projectInfoDesc, prometheus.GaugeValue, 1,
			p.Key, p.Name, p.Qualifier, p.Visibility, p.Revision)

		if ts, ok := parseSonarTime(p.LastAnalysisDate); ok {
			ch <- prometheus.MustNewConstMetric(projectLastAnalysisDesc, prometheus.GaugeValue, float64(ts.Unix()), p.Key)
		}

		emitStatusMetrics(ch, projectQualityGateDesc, mv["alert_status"], qualityGateStatuses, p.Key)

		emitGauge(ch, projectBugsDesc, mv["bugs"], p.Key)
		emitGauge(ch, projectNewBugsDesc, mv["new_bugs"], p.Key)
		emitGauge(ch, projectVulnerabilitiesDesc, mv["vulnerabilities"], p.Key)
		emitGauge(ch, projectNewVulnerabilitiesDesc, mv["new_vulnerabilities"], p.Key)
		emitGauge(ch, projectHotspotsDesc, mv["security_hotspots"], p.Key)
		emitGauge(ch, projectCodeSmellsDesc, mv["code_smells"], p.Key)
		emitGauge(ch, projectCoverageDesc, mv["coverage"], p.Key)
		emitGauge(ch, projectNewCoverageDesc, mv["new_coverage"], p.Key)
		emitGauge(ch, projectDuplicationDesc, mv["duplicated_lines_density"], p.Key)
		emitGauge(ch, projectLoCDesc, mv["ncloc"], p.Key)
		emitGauge(ch, projectReliabilityDesc, mv["reliability_rating"], p.Key)
		emitGauge(ch, projectSecurityDesc, mv["security_rating"], p.Key)
		emitGauge(ch, projectMaintainabilityDesc, mv["sqale_rating"], p.Key)
	}

	return c.collectLastAnalysisStatus(ch, keys)
}

func (c *Collector) collectLastAnalysisStatus(ch chan<- prometheus.Metric, projectKeys []string) error {
	tasks, err := c.client.RecentAnalyses()
	if err != nil {
		return fmt.Errorf("ce/activity: %w", err)
	}

	latestByProject := map[string]CETask{}
	for _, t := range tasks {
		existing, ok := latestByProject[t.ComponentKey]
		// SonarQube's submittedAt is a consistently formatted ISO-8601
		// string, so lexical comparison is equivalent to chronological
		// comparison here.
		if !ok || t.SubmittedAt > existing.SubmittedAt {
			latestByProject[t.ComponentKey] = t
		}
	}

	for _, key := range projectKeys {
		task, ok := latestByProject[key]
		if !ok {
			continue
		}
		emitStatusMetrics(ch, projectLastAnalysisStatusDesc, task.Status, analysisTaskStatuses, key)
	}
	return nil
}

func (c *Collector) collectPortfolios(ch chan<- prometheus.Metric) error {
	portfolios, err := c.client.SearchPortfolios()
	if err != nil {
		return fmt.Errorf("components/search (VW): %w", err)
	}
	if len(portfolios) == 0 {
		// Expected on non-Enterprise/Governance instances; not an error.
		return nil
	}

	keys := make([]string, len(portfolios))
	for i, p := range portfolios {
		keys[i] = p.Key
	}

	measures, err := c.client.SearchMeasures(keys, portfolioMetricKeys)
	if err != nil {
		return fmt.Errorf("measures/search (portfolios): %w", err)
	}

	values := pivotMeasures(measures, nil)

	for _, p := range portfolios {
		mv := values[p.Key]

		ch <- prometheus.MustNewConstMetric(portfolioInfoDesc, prometheus.GaugeValue, 1, p.Key, p.Name)

		emitStatusMetrics(ch, portfolioQualityGateDesc, mv["alert_status"], qualityGateStatuses, p.Key)

		emitGauge(ch, portfolioBugsDesc, mv["bugs"], p.Key)
		emitGauge(ch, portfolioVulnerabilitiesDesc, mv["vulnerabilities"], p.Key)
		emitGauge(ch, portfolioCoverageDesc, mv["coverage"], p.Key)
		emitGauge(ch, portfolioDuplicationDesc, mv["duplicated_lines_density"], p.Key)
		emitGauge(ch, portfolioLoCDesc, mv["ncloc"], p.Key)
		emitGauge(ch, portfolioReliabilityDesc, mv["reliability_rating"], p.Key)
		emitGauge(ch, portfolioSecurityDesc, mv["security_rating"], p.Key)
	}

	return nil
}

// pivotMeasures reshapes SonarQube's row-per-(component,metric) measures
// response into component -> metric -> value, merging one or two measure
// sets (overall + optional new-code). newCode may be nil.
func pivotMeasures(overall, newCode []Measure) map[string]map[string]string {
	values := map[string]map[string]string{}
	add := func(measures []Measure) {
		for _, m := range measures {
			if values[m.Component] == nil {
				values[m.Component] = map[string]string{}
			}
			values[m.Component][m.Metric] = m.EffectiveValue()
		}
	}
	add(overall)
	add(newCode)
	return values
}

// emitStatusMetrics emits one metric per possible status value (the
// kube_pod_status_phase convention): 1 for the status that's currently
// active, 0 for every other possible value. Skips entirely if current is
// empty (metric unavailable for this component).
func emitStatusMetrics(ch chan<- prometheus.Metric, d *prometheus.Desc, current string, allStatuses []string, labels ...string) {
	if current == "" {
		return
	}
	for _, status := range allStatuses {
		v := 0.0
		if current == status {
			v = 1.0
		}
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, append(append([]string{}, labels...), status)...)
	}
}

// emitGauge parses raw as a float and emits it, or skips silently if raw is
// empty/unparseable (e.g. coverage not computed for a project with no
// coverage tool configured) -- deliberately not emitted as 0, which would be
// a false signal.
func emitGauge(ch chan<- prometheus.Metric, d *prometheus.Desc, raw string, labels ...string) {
	if raw == "" {
		return
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...)
}
