package rules

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	heapDumpPathRe     = regexp.MustCompile(`-XX:HeapDumpPath=(\S+)`)
	terminationGraceRe = regexp.MustCompile(`terminationGracePeriodSeconds:`)
)

// CheckOrchestrationGracePeriodAndHeapDump is informational, not a misconfiguration
// finding: it surfaces a chart limitation the customer cannot fix through values.yaml
// today, only work around. If a rendered StatefulSet's JAVA_TOOL_OPTIONS points
// -XX:HeapDumpPath at the same path the container also mounts as a volume (the Raft/
// RocksDB data volume), a crash-loop OOM can fill that disk with heap dumps; combined
// with no terminationGracePeriodSeconds override (Kubernetes' 30s default applies to
// every pod deletion), a rolling upgrade or node drain risks an unclean broker
// shutdown. Only fires when at least one of those two conditions is actually true in
// the rendered manifest — a future chart version that fixes either one stops
// triggering this finding on its own.
func CheckOrchestrationGracePeriodAndHeapDump(docs []ManifestDoc) []Finding {
	var findings []Finding
	for _, d := range docs {
		if d.Kind != "StatefulSet" {
			continue
		}
		m := heapDumpPathRe.FindStringSubmatch(d.Text)
		if m == nil {
			continue // not the orchestration/Zeebe broker StatefulSet
		}
		heapPath := m[1]
		hasGrace := terminationGraceRe.MatchString(d.Text)
		heapOnMountedVolume := strings.Contains(d.Text, "mountPath: "+heapPath)

		if hasGrace && !heapOnMountedVolume {
			continue
		}

		var parts []string
		if !hasGrace {
			parts = append(parts, "No terminationGracePeriodSeconds override exists in this "+
				"chart version, so pod deletion (rolling upgrade, node drain) gets only "+
				"Kubernetes' 30-second default to cleanly step down as Raft leader and flush state.")
		}
		if heapOnMountedVolume {
			parts = append(parts, fmt.Sprintf(
				"JVM heap dumps on OutOfMemoryError are written to %s, which this StatefulSet also "+
					"mounts as a persistent volume — an OOM crash-loop can fill that disk with "+
					"uncollected .hprof files and trip the broker's own disk-watermark backpressure.",
				heapPath))
		}
		findings = append(findings, Finding{
			RuleID:   "CCD015",
			Severity: Low,
			Title:    fmt.Sprintf("%s/%s: known chart limitation, not a misconfiguration you made", d.Kind, d.Name),
			Path:     d.Kind + "/" + d.Name,
			Detail:   strings.Join(parts, " "),
			Remediation: "Work around this via orchestration.env (override JAVA_TOOL_OPTIONS to " +
				"redirect HeapDumpPath to /tmp) and/or orchestration.javaOpts; there is no values.yaml " +
				"field for terminationGracePeriodSeconds in this chart version, so raising it requires " +
				"a chart change upstream.",
		})
	}
	return findings
}
