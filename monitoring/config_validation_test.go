package monitoring_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type prometheusRules struct {
	Groups []ruleGroup `yaml:"groups"`
}

type ruleGroup struct {
	Name  string      `yaml:"name"`
	Rules []alertRule `yaml:"rules"`
}

type alertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

type prometheusConfig struct {
	RuleFiles     []string           `yaml:"rule_files"`
	ScrapeConfigs []prometheusScrape `yaml:"scrape_configs"`
}

type prometheusScrape struct {
	JobName       string                   `yaml:"job_name"`
	StaticConfigs []prometheusStaticConfig `yaml:"static_configs"`
}

type prometheusStaticConfig struct {
	Labels map[string]string `yaml:"labels"`
}

type grafanaDashboard struct {
	Title  string         `json:"title"`
	UID    string         `json:"uid"`
	Panels []grafanaPanel `json:"panels"`
}

type grafanaPanel struct {
	Title   string          `json:"title"`
	Type    string          `json:"type"`
	Targets []grafanaTarget `json:"targets"`
}

type grafanaTarget struct {
	Expr string `json:"expr"`
}

func TestPrometheusLoadsAlertRules(t *testing.T) {
	t.Parallel()

	var cfg prometheusConfig
	readYAML(t, "prometheus.yml", &cfg)

	if !containsString(cfg.RuleFiles, "/etc/prometheus/alerts.yml") {
		t.Fatalf("prometheus.yml rule_files = %#v, want /etc/prometheus/alerts.yml", cfg.RuleFiles)
	}
}

func TestRequiredAlertRules(t *testing.T) {
	t.Parallel()

	var rules prometheusRules
	readYAML(t, "alerts.yml", &rules)
	byName := alertRulesByName(rules)

	cases := []struct {
		alert    string
		severity string
		forValue string
		contains []string
	}{
		{
			alert:    "AgenticEcommerceHighApiLatency",
			severity: "warning",
			forValue: "5m",
			contains: []string{
				"histogram_quantile",
				"agentic_ecommerce_http_request_duration_seconds_bucket",
				"> 0.5",
			},
		},
		{
			alert:    "AgenticEcommerceHighErrorRate",
			severity: "warning",
			forValue: "5m",
			contains: []string{
				"agentic_ecommerce_http_requests_total{code=~\"5..\"}",
				"> 0.01",
			},
		},
		{
			alert:    "AgenticEcommerceSyncLagHigh",
			severity: "warning",
			forValue: "5m",
			contains: []string{
				"agentic_ecommerce_sync_lag_seconds",
				"> 300",
			},
		},
		{
			alert:    "AgenticEcommerceAgentFailureRateHigh",
			severity: "warning",
			forValue: "10m",
			contains: []string{
				"agentic_ecommerce_agent_worker_runs_total{status=\"failed\"}",
				"agentic_ecommerce_agent_worker_runs_total",
				"> 0.05",
			},
		},
		{
			alert:    "AgenticEcommerceScheduledAgentFailuresHigh",
			severity: "warning",
			forValue: "5m",
			contains: []string{
				"agentic_ecommerce_agent_scheduled_runs_total{status=\"failed\"}",
				"> 0",
			},
		},
		{
			alert:    "AgenticEcommerceComplianceFailureSpike",
			severity: "warning",
			forValue: "5m",
			contains: []string{
				"agentic_ecommerce_compliance_failures_total",
				"agentic_ecommerce_agent_worker_compliance_failures_total",
				"> 5",
			},
		},
		{
			alert:    "AgenticEcommerceRAGSearchLatencyHigh",
			severity: "warning",
			forValue: "5m",
			contains: []string{
				"histogram_quantile",
				"agentic_ecommerce_rag_search_duration_seconds_bucket",
				"> 1",
			},
		},
		{
			alert:    "AgenticEcommerceEmbeddingFailuresHigh",
			severity: "warning",
			forValue: "5m",
			contains: []string{
				"agentic_ecommerce_embedding_failures_total",
				"> 0",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.alert, func(t *testing.T) {
			rule, ok := byName[tc.alert]
			if !ok {
				t.Fatalf("missing alert rule %q", tc.alert)
			}
			if rule.For != tc.forValue {
				t.Fatalf("%s for = %q, want %q", tc.alert, rule.For, tc.forValue)
			}
			if got := rule.Labels["severity"]; got != tc.severity {
				t.Fatalf("%s severity = %q, want %q", tc.alert, got, tc.severity)
			}
			if strings.TrimSpace(rule.Annotations["summary"]) == "" {
				t.Fatalf("%s summary annotation is empty", tc.alert)
			}
			for _, want := range tc.contains {
				if !strings.Contains(rule.Expr, want) {
					t.Fatalf("%s expr missing %q:\n%s", tc.alert, want, rule.Expr)
				}
			}
		})
	}
}

func TestGrafanaDashboardCoversV080ObservabilityViews(t *testing.T) {
	t.Parallel()

	var dashboard grafanaDashboard
	readJSON(t, "grafana/dashboards/agentic-ecommerce-overview.json", &dashboard)

	if dashboard.UID != "agentic-ecommerce-overview" {
		t.Fatalf("dashboard uid = %q, want agentic-ecommerce-overview", dashboard.UID)
	}

	cases := []struct {
		name     string
		title    string
		contains []string
	}{
		{
			name:  "api latency",
			title: "API p95 Latency",
			contains: []string{
				"histogram_quantile",
				"agentic_ecommerce_http_request_duration_seconds_bucket",
			},
		},
		{
			name:  "api error rate",
			title: "API 5xx Error Rate",
			contains: []string{
				"agentic_ecommerce_http_requests_total{code=~\"5..\"}",
			},
		},
		{
			name:  "sync lag",
			title: "WooCommerce Sync Lag",
			contains: []string{
				"agentic_ecommerce_sync_lag_seconds",
			},
		},
		{
			name:  "sync conflicts",
			title: "WooCommerce Sync Conflicts",
			contains: []string{
				"agentic_ecommerce_sync_conflicts_total",
			},
		},
		{
			name:  "agent runs",
			title: "Agent Runs Success vs Failure",
			contains: []string{
				"agentic_ecommerce_agent_worker_runs_total{status=\"succeeded\"}",
				"agentic_ecommerce_agent_worker_runs_total{status=\"failed\"}",
			},
		},
		{
			name:  "compliance rates",
			title: "Compliance Pass vs Fail Rate",
			contains: []string{
				"agentic_ecommerce_compliance_checks_total",
				"agentic_ecommerce_compliance_failures_total",
			},
		},
		{
			name:  "rag search latency",
			title: "RAG Search p95 Latency",
			contains: []string{
				"histogram_quantile",
				"agentic_ecommerce_rag_search_duration_seconds_bucket",
			},
		},
		{
			name:  "embedding failures",
			title: "Embedding Bridge Failures",
			contains: []string{
				"agentic_ecommerce_embedding_failures_total",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			panel, ok := dashboardPanelByTitle(dashboard, tc.title)
			if !ok {
				t.Fatalf("missing dashboard panel %q", tc.title)
			}
			targetExprs := strings.Join(panelExpressions(panel), "\n")
			for _, want := range tc.contains {
				if !strings.Contains(targetExprs, want) {
					t.Fatalf("panel %q missing query fragment %q:\n%s", tc.title, want, targetExprs)
				}
			}
		})
	}
}

func TestMonitoringDoesNotUseRawTenantIDAsMetricDimension(t *testing.T) {
	t.Parallel()

	var cfg prometheusConfig
	readYAML(t, "prometheus.yml", &cfg)
	for _, scrape := range cfg.ScrapeConfigs {
		for _, staticConfig := range scrape.StaticConfigs {
			if _, ok := staticConfig.Labels["tenant_id"]; ok {
				t.Fatalf("scrape job %q uses raw tenant_id label; use bounded dimensions only", scrape.JobName)
			}
		}
	}

	var rules prometheusRules
	readYAML(t, "alerts.yml", &rules)
	for _, group := range rules.Groups {
		for _, rule := range group.Rules {
			if strings.Contains(rule.Expr, "tenant_id") {
				t.Fatalf("alert %q groups or filters by raw tenant_id:\n%s", rule.Alert, rule.Expr)
			}
		}
	}

	var dashboard grafanaDashboard
	readJSON(t, "grafana/dashboards/agentic-ecommerce-overview.json", &dashboard)
	for _, panel := range dashboard.Panels {
		for _, expr := range panelExpressions(panel) {
			if strings.Contains(expr, "tenant_id") {
				t.Fatalf("dashboard panel %q queries raw tenant_id:\n%s", panel.Title, expr)
			}
		}
	}
}

func readYAML(t *testing.T, path string, dest any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.Unmarshal(data, dest); err != nil {
		t.Fatalf("parse YAML %s: %v", path, err)
	}
}

func readJSON(t *testing.T, path string, dest any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		t.Fatalf("parse JSON %s: %v", path, err)
	}
}

func alertRulesByName(rules prometheusRules) map[string]alertRule {
	byName := make(map[string]alertRule)
	for _, group := range rules.Groups {
		for _, rule := range group.Rules {
			if rule.Alert != "" {
				byName[rule.Alert] = rule
			}
		}
	}
	return byName
}

func dashboardPanelByTitle(dashboard grafanaDashboard, title string) (grafanaPanel, bool) {
	for _, panel := range dashboard.Panels {
		if panel.Title == title {
			return panel, true
		}
	}
	return grafanaPanel{}, false
}

func panelExpressions(panel grafanaPanel) []string {
	exprs := make([]string, 0, len(panel.Targets))
	for _, target := range panel.Targets {
		exprs = append(exprs, target.Expr)
	}
	return exprs
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
