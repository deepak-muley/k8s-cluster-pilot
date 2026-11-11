# AGENTS.md

Guidance for coding agents working in this repository. People should start with the [README](README.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

## Commands

Go 1.27.3 or newer. Module path: `github.com/deepak-muley/k8s-cluster-pilot`. The `go` line in `go.mod` selects the toolchain. An older `go` binary downloads it; `go version` outside the module can still report the older install.

```bash
go test ./...
go build -o bin/clusterpilot ./cmd/clusterpilot
gofmt -w .
```

`make test`, `make build`, and `make fmt` wrap those commands. Run `go test ./...` before finishing a change. CI runs the same test command. `make test-kind` is optional: it needs Docker, kind, kubectl, and Helm, and it is documented in `test/kind/README.md`. `make test-all` runs `make test` and then `make test-kind`.

Do not commit `bin/`, `inventory.json`, or `plan.json`. Discovery output can describe a real cluster.

## Layout

| Path | Role |
|---|---|
| `cmd/clusterpilot` | Process entrypoint |
| `internal/cli` | Cobra commands and help text |
| `internal/discover` | Read-only Kubernetes and Helm collection |
| `internal/inventory` | Versioned inventory contract, schema `v1alpha1` |
| `internal/knowledge` | Loads builtin and extra YAML rules |
| `knowledge/rules` | Builtin skew and operations rules, embedded by `knowledge/embed.go` |
| `knowledge/pluto` | Pinned Pluto `versions.yaml` and the release named in `source.yaml` |
| `scripts/update-pluto-versions.sh` | Replace the Pluto pin from a release tag. Tags are listed at <https://github.com/FairwindsOps/pluto/releases> |
| `test/kind` | Optional kind cluster scenarios. Not part of `go test ./...` |
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
- Do not hand-edit `knowledge/pluto/versions.yaml`. Update it with `make update-pluto VERSION=<tag>`, using a tag from <https://github.com/FairwindsOps/pluto/releases>. `source.yaml` records that tag and the file checksum. Load only `component: k8s` rows. Istio and cert-manager rows are not Kubernetes versions.
- With `--output` set, `discover` writes the inventory file and leaves stdout empty. Warnings go to stderr.

## Report output

`internal/report` renders the human-readable analysis and plan. Keep findings grouped, with shared severity, confidence, and verification stated once per group. Put the explanation in the detail column. `report --format markdown` uses the same grouping as Markdown tables.
