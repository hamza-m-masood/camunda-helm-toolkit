// Package monitorgen generates ServiceMonitor and PrometheusRule manifests from a
// chart's ACTUAL rendered Service objects, rather than hardcoded port-name guesses —
// the audit that motivated this tool found a real ServiceMonitor shipping with a port
// name that didn't exist on its own Service, so this package refuses to repeat that
// mistake by construction: it only ever names a port it already found rendered.
package monitorgen

import (
	"gopkg.in/yaml.v3"

	"github.com/hamza-m-masood/camunda-chart-doctor/internal/rules"
)

// ServiceInfo is what this package needs from one rendered Service object.
type ServiceInfo struct {
	Name     string
	Labels   map[string]string
	Selector map[string]string
	Ports    []string // port names only; unnamed ports are skipped (ServiceMonitor targets by name)
}

type serviceDoc struct {
	Metadata struct {
		Name   string            `yaml:"name"`
		Labels map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		Selector map[string]string `yaml:"selector"`
		Ports    []struct {
			Name string `yaml:"name"`
		} `yaml:"ports"`
	} `yaml:"spec"`
}

// DiscoverServices parses every rendered Service document and returns its name,
// selector/labels, and named ports — the only inputs a correct ServiceMonitor needs.
func DiscoverServices(docs []rules.ManifestDoc) []ServiceInfo {
	var out []ServiceInfo
	for _, d := range docs {
		if d.Kind != "Service" {
			continue
		}
		var sd serviceDoc
		if err := yaml.Unmarshal([]byte(d.Text), &sd); err != nil {
			continue // malformed doc — skip rather than emit a guess
		}
		var ports []string
		for _, p := range sd.Spec.Ports {
			if p.Name != "" {
				ports = append(ports, p.Name)
			}
		}
		if len(ports) == 0 {
			continue
		}
		out = append(out, ServiceInfo{
			Name:     sd.Metadata.Name,
			Labels:   sd.Metadata.Labels,
			Selector: sd.Spec.Selector,
			Ports:    ports,
		})
	}
	return out
}

// metricsPortCandidates ranks likely metrics-port names, most-specific first — this
// chart's own convention (confirmed by rendering) uses "metrics", "management", or a
// component-configurable name that resolves to one of these by default.
var metricsPortCandidates = []string{"metrics", "management", "http-management", "server"}

// PickMetricsPort returns the best-matching port name found on a Service, and false
// if none of the known conventions matched — callers must skip the Service entirely
// in that case rather than guess at an arbitrary port.
func PickMetricsPort(s ServiceInfo) (string, bool) {
	for _, candidate := range metricsPortCandidates {
		for _, p := range s.Ports {
			if p == candidate {
				return p, true
			}
		}
	}
	return "", false
}
