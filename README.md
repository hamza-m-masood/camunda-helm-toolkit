# camunda-chart-doctor

> **Alpha / experimental — unofficial, community-maintained.** This is not an official
> Camunda product, has not been through Camunda's product or security review process,
> and carries no support guarantee. It is built and validated by one engineer. Read the
> [Disclaimer](#disclaimer) before pointing it at anything you care about.

A pre-flight, live, upgrade, and bootstrap checker for
[Camunda 8 Self-Managed](https://camunda.com) Helm installs. Three core commands, plus
[several more](#other-commands) that turn a finding into a fix instead of just a report:

- **`init`** builds a starter values.yaml from a handful of questions, and — this is the
  actual point of it — refuses to hand you the result unless it already passes every
  `check` rule this tool can close via values.yaml alone. Most values.yaml generators
  stop at "does this parse"; this one stops at "does this pass the audit."
- **`check`** statically scans a chart + values overlay (or an already-installed Helm
  release, optionally cross-checked against the live cluster) for recurring Kubernetes
  misconfigurations that are individually easy to make, hard to notice, and expensive to
  hit in production: burstable memory QoS on a stateful broker, a disabled
  PodDisruptionBudget on a quorum-based cluster, a secret reference that silently no-ops,
  default credentials left in place, disabled log/index retention, and more.
- **`upgrade`** answers a different question: *what will break if I move this release to
  a newer chart line?* It reads the values you actually supplied, reports every
  values.yaml key the target line removed, renamed or deprecated, rewrites what it can
  rewrite, and prints a runbook for the imperative steps no values file can express —
  most notably the Keycloak/PostgreSQL data migration that 8.10 requires. It never writes
  to your cluster.

See [Checks](#checks) for the full list and the failure mode each one guards against.

Passing every check is **not** proof a deployment is production-ready or that an upgrade
will succeed — it only means this specific, known set of footguns isn't present.

## Install

Download a binary from the [Releases](../../releases) page for your platform, or build
from source:

```sh
go install github.com/hamza-m-masood/camunda-chart-doctor/cmd/camunda-chart-doctor@latest
```

Requires `helm` on `PATH` for chart-based checks and for `upgrade --release`, and
`kubectl` or `oc` on `PATH` for `--live` checks. The migration data is compiled into the
binary, so `upgrade` needs no chart checkout.

## Init

Build a starter values.yaml, answering questions on the command line:

```sh
camunda-chart-doctor init --chart ./camunda-platform
```

Or fully non-interactively, for CI or scripting — every question has a flag, and
`--non-interactive` fails on any missing required one instead of prompting:

```sh
camunda-chart-doctor init --chart ./camunda-platform --non-interactive \
  --release-name camunda --secondary-storage elasticsearch \
  --auth-method basic --admin-username admin --admin-password '<a real password>' \
  --enable connectors \
  --throughput 800 --avg-payload-kb 3 \
  --write-values my-values.yaml
```

Before printing or writing anything, `init` runs the exact same rules `check` would run
against the result — both the values-based rules and, since `--chart` is required, the
manifest-based ones too, against a real `helm template` render — and refuses to produce
output at all if anything comes back that it can't explain. Three findings are
structurally impossible for it to close and are the only ones ever expected: digest
pinning (CCD008 — the right digest is specific to the exact release you install), a
chart-template limitation with no values.yaml field at all (CCD015), and one pre-existing
8.9 chart-default defect unrelated to anything `init` controls (CCD003 on
`identityKeycloak.*`). Anything else that fires is a bug in `init`, not something it will
hand you — see [How this was built](#how-this-was-built) for how that guarantee is tested.

`--enable` is repeatable (`identity`, `connectors`, `optimize`, `webModeler`); enabling
`webModeler` also requires `--web-modeler-from-email` (the chart's own hard render
requirement, not this tool's). Sizing (`--throughput`/`--avg-payload-kb`) calls the same
heuristic `size` uses — see that command's own section for what the numbers mean.

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

**Plan an upgrade** — point it at an installed release and say where you want to go. It
detects which chart line you are on from the release itself:

```sh
camunda-chart-doctor upgrade --release my-camunda -n my-namespace --to 8.10
```

Save the rewritten values alongside the report, and optionally drop the keys the target
line removed:

```sh
camunda-chart-doctor upgrade --release my-camunda -n my-namespace \
  --to 8.10 --write-values migrated-values.yaml --strip-removed
```

Renames are always applied (the replacement key is known). Deleting a setting is a
judgement call about intent, so `--strip-removed` is opt-in.

**Plan without a cluster** — from a values file you already have. A values file does not
record its own chart version, so `--from` is required:

```sh
camunda-chart-doctor upgrade -f my-values.yaml --from 8.9 --to 8.10
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
| CCD013 | Medium | `prometheusServiceMonitor.enabled` left at its default `false` — zero scrape targets, so nothing else here is observable either. |
| CCD014 | Medium | A bundled `identityPostgresql`/`webModelerPostgresql` subchart is enabled with its own `backup.enabled` left off (a logical-dump-only mechanism, not PITR, but better than nothing). |
| CCD015 | Low | Informational, not a misconfiguration you made: the chart's own rendered StatefulSet has no `terminationGracePeriodSeconds` and/or writes JVM heap dumps onto the same volume as the Raft/RocksDB data. Framed around the `orchestration.env`/`javaOpts` workaround, since there's no values.yaml field for the grace period itself. |
| CCD016 | Low | Default broker anti-affinity still matches only `app.kubernetes.io/component`, with no `app.kubernetes.io/instance` — a second release in the same namespace can't co-schedule, and a drain has nowhere else to place an evicted broker on a minimal node pool. |

\* Severity reflects likely blast radius and how easy the misconfiguration is to make —
not a guarantee about your specific environment.

### Upgrade checks (`upgrade`)

| ID | Severity | What it catches |
|----|----------|------------------|
| CCD101 | High | A values key the target line **removed**. The chart calls `fail()`, so `helm upgrade` renders nothing until the key is gone. |
| CCD102 | High | A values key the target line **renamed**. Also a hard render failure, but mechanically fixable — the rewrite is applied for you, including subtree renames like `camundaHub.webModeler.*` → `camundaHub.*`. |
| CCD103 | Low | A **deprecated** key. The upgrade still succeeds; the chart keeps warning, and names the release it is scheduled for removal in. |
| CCD104 | Critical | Bundled Bitnami subcharts (Keycloak / PostgreSQL / Elasticsearch) are enabled and the target line removes them. Your Keycloak realm and the Identity and Web Modeler databases live inside those workloads — upgrading before moving that data deletes what holds it. |
| CCD105 | High | The requested jump spans more than one minor. Camunda does not support skipping minors, so this is N separate upgrades, each of which must land healthy before the next. |
| CCD106 | Medium | A component's `env` / `extraConfiguration` / `command` / `extraVolumeMounts` is set. The chart cannot validate these, so they are invisible to every other check — and an override that works around a limitation the target version fixes internally can conflict after upgrading. |
| CCD107 | Medium | The target chart requires a newer Helm CLI major than the constraint allows (chart 15.x requires Helm v4). Fails at render time, before anything is applied. |
| CCD108 | Low | The line you are leaving is end-of-life. |
| CCD109 | Medium | No migration data is embedded for one of the hops, so findings are incomplete for it. See [Upgrade coverage](#upgrade-coverage). |

### Upgrade coverage

The key data is **generated from the chart's own deprecation helpers**
(`camundaPlatform.keyRemoved`, `keyRenamed`, `keyDeprecated` in
`templates/**/constraints.tpl`) rather than maintained by hand, so this tool cannot
disagree with the chart about what changed, and every finding cites the chart file and
line it came from. Regenerate with `camunda-chart-doctor generate --chart-repo <path>`.

Those helpers were introduced in the 8.8 chart. **Hops into 8.8, 8.9 and 8.10 are
covered; hops into 8.7 and earlier are not** — those charts express deprecations as
ad-hoc warning strings with no machine-readable structure. Planning an upgrade that
passes through an uncovered hop raises CCD109 rather than implying the hop is clean.

Where the chart's condition for a key is too complex to model exactly (a compound
`and`/`or`), the finding is reported on key presence alone and the report says how many
findings are approximate. It errs toward showing you a key you may not need to change,
rather than hiding one you do.

### What the upgrade command does not do

It does not upgrade anything. It reads `helm get values` and `helm get metadata`, and
everything it suggests is printed for you to run, labelled `safe`, `destructive`, or
`downtime`. A clean render is also not a safe upgrade: CCD104 in particular describes a
data migration that has to happen between two Helm operations, and no amount of
values rewriting substitutes for it.

## Suppressing a finding

A `.chartdoctor-ignore.yaml` in the current directory (or passed via `--ignore-file`) is
auto-loaded by `check`:

```yaml
suppress:
  - ruleId: CCD002
    path: identity.resources # optional; exact match or dot-prefix match
    reason: "identity is intentionally run Burstable in this cluster — accepted 2026-08."
```

`reason` is required — a suppression file stays self-documenting. Suppressed findings are
never silently dropped: the summary line always shows a count, and `--show-suppressed`
lists them in full.

## Output formats

`--json` for machine-readable output, or `--format sarif` for GitHub code-scanning / PR
annotations instead of plain CI log text. `--fail-on critical|high|medium|low` controls
which severities cause a nonzero exit code (default `high`).

## GitHub Action

```yaml
- uses: hamza-m-masood/camunda-chart-doctor@v0.3.0-alpha
  with:
    chart: ./camunda-platform
    values: |
      values.yaml
      values-prod.yaml
    fail-on: high
```

Builds the tool from source at the pinned ref (not a downloaded release asset, so it
always matches exactly what that ref's source produces) and runs `check` with inputs
mirroring the CLI flags (`release`, `namespace`, `live`, `format`, `ignore-file`).
Outputs `findings-file` (path to the raw output) and `exit-code`.

## Other commands

Beyond `check`, `upgrade`, and `generate` (see above), a few more:

**`plan-secrets --release <name> -n <namespace>`** — finds chart-managed Secrets not yet
pinned via `existingSecret` and prints the exact fix: a command to copy the Secret's
current value to a new, independently-named object the chart will never own or prune
(pointing `existingSecret` at the *same* name it already uses does **not** work — the
chart stops rendering that Secret once every field is pinned, and Helm then deletes it),
plus the values overlay to apply.

**`bundle --release <name> -n <namespace> -o <path.tar.gz>`** (`--dry-run` to preview) —
a support-ticket-ready archive: live findings, a **redacted** `helm get values -a`
(every password/secret/token/credential/`*Key` value replaced with `<redacted>`),
describe/events/logs for the orchestration component, and version info — all listed in
a `manifest.json` so you can see exactly what you're about to send.

**`scaffold-monitoring --release <name> -n <namespace> --chart <path>`** — generates
ServiceMonitor manifests using port names taken from the chart's own rendered Service
objects (never hardcoded), plus a baseline PrometheusRule. Output only; review and
`kubectl apply` yourself.

**`size --throughput <cmds/sec> --avg-payload-kb <n> [--retention-days <n>]`** — a
heuristic (not a certified benchmark) starting point for `clusterSize`/`partitionCount`/
`pvcSize`/resources, with the arithmetic behind every number printed alongside it.

**`scaffold-watcher --release <name> -n <namespace> --schedule "<cron>"`** — generates a
CronJob + least-privilege RBAC that reruns `check --live` on a schedule and reports only
*new* findings (via a state ConfigMap), optionally to `--webhook-url`. Needs the image
built from this repo's `Dockerfile` (published to `ghcr.io/hamza-m-masood/camunda-chart-doctor`
on tag push).

## Exit codes

`0` clean (at or below the `--fail-on` threshold) · `1` worst finding is medium/low ·
`2` worst finding is high · `3` worst finding is critical. All commands use the same
scale.

## How this was built

Every `check` rule traces back to a documented Kubernetes failure mode, verified against
the actual Camunda Platform Helm chart templates and by rendering/deploying the chart —
not inferred from documentation. The `upgrade` command's key data is generated from those
same chart templates, and its runbook steps are transcribed from the chart repo's own CI
upgrade hooks — so each one is a step the chart's test matrix actually runs, and carries
a `source:` pointing at it. Verified end to end against a real 8.9 install on OpenShift:
the unmigrated values fail the 8.10 render on exactly the keys reported, and the migrated
values render clean. None of it encodes anything specific to any customer,
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
