# AGENTS.md

Guidance for coding agents working in this repository. People should start with the [README](README.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

## Commands

Go 1.24 or newer. Module path: `github.com/deepak-muley/k8s-cluster-pilot`.

```bash
go test ./...
go build -o bin/clusterpilot ./cmd/clusterpilot
gofmt -w .
```

`make test`, `make build`, and `make fmt` wrap those commands. Run `go test ./...` before finishing a change. CI runs the same test command.

Do not commit `bin/`, `inventory.json`, or `plan.json`. Discovery output can describe a real cluster.

## Layout

| Path | Role |
|---|---|
| `cmd/clusterpilot` | Process entrypoint |
| `internal/cli` | Cobra commands and help text |
| `internal/discover` | Read-only Kubernetes and Helm collection |
| `internal/inventory` | Versioned inventory contract, schema `v1alpha1` |
| `internal/knowledge` | Loads builtin and extra YAML rules |
| `knowledge/rules` | Builtin rules, embedded by `knowledge/embed.go` |
| `internal/analyze` | Offline compatibility evaluation |
| `internal/plan` | Upgrade plan built from analysis and the graph |
| `internal/graph` | `upgrade-before` edges only when a rule requires that order |
| `internal/report` | Text tables and Markdown for analysis and plan documents |
| `internal/model` | Analysis and plan document, schema `v1alpha1` |

`discover` is the only command that connects to a cluster. `analyze`, `plan`, and `report` read files.

## Invariants

- Discovery lists and gets objects. Do not call create, update, patch, delete, delete-collection, or apply. `TestCollectorsAreReadOnly` rejects those calls under `internal/discover`.
- Do not store Secret data, ConfigMap data, environment values, webhook CA bundles, or Helm values.
- A missing rule is `unknown`, not `compatible`. Do not infer CNI, CSI, runtime, CoreDNS, etcd, or Helm compatibility.
- A chart `kubeVersion` that includes the target stays `unknown`. A constraint that excludes the target is `potential-risk`. Both are publisher claims.
- `upgrade-before` edges exist only for a loaded rule, and each edge carries that rule ID. Structural edges do not imply an upgrade order.
- The plan does not invent patch numbers. A target of `1.33` means minor 1.33.
- A new or changed builtin rule needs a stable `id`, a `source.url`, `retrievedAt`, and a test that fails if the rule is dropped or inverted.
- With `--output` set, `discover` writes the inventory file and leaves stdout empty. Warnings go to stderr.

## Report output

`internal/report` renders the human-readable analysis and plan. Keep findings grouped, with shared severity, confidence, and verification stated once per group. Put the explanation in the detail column. `report --format markdown` uses the same grouping as Markdown tables.
