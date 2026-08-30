# Getting started

A walkthrough of the whole toolkit in the order you'd actually use it: from a blank
directory to a running, hardened Camunda 8 Self-Managed install, through your first
upgrade. Each step links to the [README](README.md#checks) for the detail it skips.

Every command below also works fully non-interactively — pass every flag it would
otherwise prompt for, or add `--non-interactive` — so everything here is scriptable for
CI once you've worked out your own answers by hand.

## 0. Install

```sh
go install github.com/hamza-m-masood/camunda-helm-toolkit/cmd/camunda-helm-toolkit@latest
```

Or grab a binary from the [Releases](https://github.com/hamza-m-masood/camunda-helm-toolkit/releases)
page. You'll also need `helm` on `PATH` for everything below, and `kubectl` or `oc` once
you get to a real cluster.

## 1. Build a starter values.yaml

```sh
camunda-helm-toolkit init --chart ./camunda-platform
```

It asks a handful of questions — which components you need, your secondary storage,
expected load, ingress, and auth — and only those; you're not filling in a 400-line
form. Before it prints anything, it runs the result through every rule `check` knows and
refuses to hand you output that fails its own audit. If you'd rather skip the prompts:

```sh
camunda-helm-toolkit init --chart ./camunda-platform --non-interactive \
  --release-name camunda --secondary-storage elasticsearch \
  --auth-method basic --admin-username admin --admin-password '<a real password>' \
  --throughput 800 --avg-payload-kb 3 \
  --write-values my-values.yaml
```

The notes it prints alongside the file call out the handful of things it *can't* close
by itself (image digests, a couple of chart-level limitations) — read those once before
moving on.

## 2. Deploy it

```sh
helm install camunda ./camunda-platform -f my-values.yaml
```

Not this tool's job — `init` gets you a values file, not a running cluster.

## 3. Confirm what's actually live matches what you asked for

```sh
camunda-helm-toolkit check --release camunda -n <namespace> --live
```

`init` checked the *values*; `--live` checks the *cluster* — did the PodDisruptionBudget
actually get created, does every referenced Secret actually exist with the key you
expect. Run this after every `helm upgrade` too, not just the first install; a value can
be correct in the file and still not be what's actually running.

## 4. Pin your secrets before your first real upgrade

```sh
camunda-helm-toolkit plan-secrets --release camunda -n <namespace>
```

Some of the chart's own secrets regenerate on `helm upgrade` unless you pin them first —
this finds the ones that will and prints the exact commands to fix it. Do this once,
early, not after an upgrade has already rotated something out from under you.

## 5. Wire up monitoring

```sh
camunda-helm-toolkit scaffold-monitoring --release camunda -n <namespace> \
  --chart ./camunda-platform -f my-values.yaml
```

Prints ServiceMonitor + PrometheusRule manifests — review them, then `kubectl apply`
yourself. Nothing here auto-applies to your cluster; that's a deliberate line this
whole toolkit holds everywhere.

## 6. Set up a standing check

```sh
camunda-helm-toolkit scaffold-watcher --release camunda -n <namespace> \
  --schedule "*/30 * * * *" --webhook-url <your-slack-webhook>
```

Everything above is a point-in-time snapshot. This generates a CronJob that reruns
`check --live` on a schedule and reports only *new* findings — someone deletes your PDB
six weeks from now, you hear about it without having to remember to ask.

## 7. When you're ready to move chart versions

```sh
camunda-helm-toolkit upgrade --release camunda -n <namespace> --to 8.10 \
  --write-values migrated-values.yaml
```

Reads what you actually set, tells you every key the target line removed, renamed, or
deprecated, rewrites what it safely can, and prints a runbook for what it can't (most
notably any data migration a values rewrite can never substitute for). It never touches
your cluster — every command it suggests is one you run yourself.

## 8. If something's wrong and you need help

```sh
camunda-helm-toolkit bundle --release camunda -n <namespace> --dry-run   # see what would be collected first
camunda-helm-toolkit bundle --release camunda -n <namespace> -o support.tar.gz
```

A redacted, reviewable archive — findings, values (secrets stripped), describe/events/
logs, versions — with a manifest listing exactly what's inside, so you can check it
yourself before you attach it to anything.

---

That's the whole loop: `init` → deploy → `check --live` → `plan-secrets` →
`scaffold-monitoring` → `scaffold-watcher`, with `upgrade` and `bundle` waiting for the
two moments you'll actually need them. See the [README](README.md) for every flag and
the full rule list.
