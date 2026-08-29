# camunda-chart-doctor

> **Alpha / experimental — unofficial, community-maintained.** This is not an official
> Camunda product, has not been through Camunda's product or security review process,
> and carries no support guarantee. It is built and validated by one engineer. Read the
> [Disclaimer](#disclaimer) before pointing it at anything you care about.

A pre-flight and live checker for [Camunda 8 Self-Managed](https://camunda.com) Helm
installs. It statically scans a chart + values overlay (or an already-installed Helm
release, optionally cross-checked against the live cluster) for a set of recurring
Kubernetes misconfigurations that are individually easy to make, hard to notice, and
expensive to hit in production: burstable memory QoS on a stateful broker, a disabled
PodDisruptionBudget on a quorum-based cluster, a secret reference that silently no-ops,
default credentials left in place, disabled log/index retention, and a few more. See
[Checks](#checks) for the full list and the failure mode each one guards against.

Passing every check is **not** proof a deployment is production-ready — it only means
this specific, known set of footguns isn't present.

## Install

Download a binary from the [Releases](../../releases) page for your platform, or build
from source:

```sh
go install github.com/hamza-m-masood/camunda-chart-doctor/cmd/camunda-chart-doctor@latest
```

Requires `helm` on `PATH` for chart-based checks, and `kubectl` or `oc` on `PATH` for
`--live` checks.

## Usage

**Pre-install** — check a chart plus your values overlay(s), the same way you'd pass
them to `helm install`/`helm upgrade`:

```sh
camunda-chart-doctor check --chart ./camunda-platform -f my-values.yaml
```

**Post-install** — check an installed release using Helm's own recorded effective
values:

```sh
camunda-chart-doctor check --release my-camunda -n my-namespace
```

**Post-install + live drift check** — additionally query the cluster directly for
things values.yaml can't show (was the PodDisruptionBudget actually created? does a
referenced Secret still exist with the expected key?):

```sh
camunda-chart-doctor check --release my-camunda -n my-namespace --live
```

Add `--json` for machine-readable output, `--fail-on critical|high|medium|low` to
control which severities cause a nonzero exit code (default `high`) — useful for gating
a CI pipeline without failing the build on advisory-level findings.

## Checks

| ID | Severity* | What it catches |
|----|-----------|------------------|
| CCD001 | High | `orchestration.podDisruptionBudget.enabled` left at its default `false` — nothing stops a node drain from evicting a quorum of brokers at once. |
| CCD002 | Critical (orchestration) / Medium (elsewhere) | `resources.requests.memory` set below `resources.limits.memory` anywhere in the chart — Burstable QoS, evicted ahead of Guaranteed pods under memory pressure, and an unwarned OOMKill on any spike past the limit. |
| CCD003 | High | An `existingSecret` reference configured without its matching `existingSecretKey` — the chart silently drops the reference rather than failing, and the component falls back to an empty-string credential. |
| CCD004 | Medium | The chart's shipped example users (`demo`/`demo`, `connectors`/`connector`) still present in the initial user list. |
| CCD005 | Medium | Index/log retention (ILM) disabled — exported/archived indices grow unbounded toward the secondary storage disk. |
| CCD006 | Low | `orchestration.readinessProbe.timeoutSeconds` at `1` — inside normal JVM GC-pause range; a cluster-wide readiness flap can empty the gRPC Service's endpoint list. |
| CCD007 | High | `replicationFactor` greater than `clusterSize` — every broker crash-loops at startup on a config-validation error, while Helm reports success. |
| CCD008 | Low | Any image pinned by tag with no digest — the tag can silently move to a different image over time. |
| CCD009 | High | A rendered manifest referencing a `bitnamilegacy/*` image — Bitnami's archived, no-longer-patched registry. |
| CCD010 | High | A rendered ConfigMap embedding what looks like a literal (non-templated) password or client secret. |
| CCD011 | High / Medium | `--live` only: no PodDisruptionBudget object actually exists for the orchestration component, or an existing one currently allows zero disruptions. |
| CCD012 | High | `--live` only: a referenced Secret or key declared in values.yaml doesn't actually exist in the cluster (renamed, deleted, or never created). |

\* Severity reflects likely blast radius and how easy the misconfiguration is to make —
not a guarantee about your specific environment.

## Exit codes

`0` clean (at or below the `--fail-on` threshold) · `1` worst finding is medium/low ·
`2` worst finding is high · `3` worst finding is critical.

## How this was built

Every check here traces back to a documented Kubernetes failure mode, verified against
the actual Camunda Platform Helm chart templates and by rendering/deploying the chart —
not inferred from documentation. None of it encodes anything specific to any customer,
deployment, or incident; the checks are general Kubernetes/Helm patterns (Burstable QoS,
PodDisruptionBudget coverage, secret-reference validation) applied to this chart's
specific value paths.

## Disclaimer

This tool is provided as-is, with no warranty of any kind, and no commitment to keep it
updated as the underlying Helm chart evolves. It is **not** built, reviewed, or endorsed
by Camunda. A clean run does not mean your deployment is secure, highly available, or
ready for production traffic — it means the specific checks above didn't fire. Always
validate changes in a non-production environment first, and treat this as one input
among several (chart documentation, your own architecture review, load testing) rather
than a substitute for any of them.

## License

Apache 2.0 — see [LICENSE](LICENSE).
