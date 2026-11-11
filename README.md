# ClusterPilot

ClusterPilot is a read-only Kubernetes upgrade intelligence CLI. It discovers a cluster, writes a versioned inventory, and evaluates that inventory offline against a target Kubernetes version. Every compatibility result is either backed by a loaded rule or explicitly unknown. Missing evidence is not treated as compatibility.

This POC does not change a cluster. Discovery lists Kubernetes objects and Helm release metadata. It does not store Secret data, ConfigMap data, environment values, webhook CA bundles, or Helm values.

## Commands

```bash
clusterpilot discover --context kind-dev --output inventory.json

clusterpilot analyze \
  --inventory inventory.json \
  --target-kubernetes 1.33

clusterpilot plan \
  --inventory inventory.json \
  --target-kubernetes 1.33 \
  --output plan.json

clusterpilot report --input plan.json --format markdown
```

`analyze` and `plan` do not connect to a cluster. Add `--rules` to merge extra YAML rule files with the builtin knowledge base.

## What a result means

| Status | Meaning |
|---|---|
| `compatible` | A loaded rule matched and the observed versions satisfy it. |
| `incompatible` | A loaded rule matched and the observed state violates it. |
| `potential-risk` | A rule or observation requires attention, but it is not a proven version break. |
| `unknown` | The inventory does not contain enough evidence, or no rule covers the component. |

Overall status:

- `blocked` — at least one finding has to be resolved before the upgrade is supported.
- `risks-present` — no hard block, but a potential risk was recorded.
- `insufficient-evidence` — no hard block, and at least one relevant check is unknown.
- `plan-available` — the direct minor jump is unsupported, and the plan sequences one minor at a time.
- `compatible` — every non-advisory check that ran was verified compatible.

`compatible` does not mean every Kubernetes API, CNI, CSI, chart, kernel, or runtime was certified. The builtin knowledge base is a dated snapshot.

## Inventory

Discovery writes schema `v1alpha1`. The inventory is the contract between discovery and the analyzer. Helm `chartVersion` and `appVersion` are stored separately. A component `version` is set only from an app version or image tag that parses as a semantic version. `latest` is not a version.

Node `reportedKernelVersion` and `osImage` are copied from kubelet status. They are not verified by a node agent.

## Knowledge base

Builtin skew and operations rules live in `knowledge/rules/` and are embedded in the binary. They were retrieved on 2026-11-09 from:

- [Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/)
- [Disruptions](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/)

The skew rules encode the published minor limits, including the smaller limit for kubelet and kube-proxy versions below 1.25.

Removed and deprecated Kubernetes APIs come from a pinned [Pluto](https://github.com/FairwindsOps/pluto) `versions.yaml`. The pin is `knowledge/pluto/source.yaml` (module, release, file URL, and sha256), and the file itself is `knowledge/pluto/versions.yaml`. Findings cite that release. Only `component: k8s` rows are loaded. Istio and cert-manager rows in the same file are versions of those products, so they are not compared with the Kubernetes target. A row with `removed-in` blocks when the API is still used. A row that is only deprecated is a potential risk. An API absent from the file was not treated as compatible.

Pluto releases are listed at <https://github.com/FairwindsOps/pluto/releases>. The file for a tag is `https://github.com/FairwindsOps/pluto/blob/<tag>/versions.yaml`. To adopt a newer release:

```bash
make update-pluto VERSION=v5.24.5
go test ./...
```

Chart `kubeVersion` is evaluated as a publisher claim. A constraint that includes the target stays `unknown`. A constraint that excludes it becomes `potential-risk`.

## Upgrade graph and plan

The plan graph has two kinds of edges:

- `upgrade-before` edges exist only when a loaded rule requires that order, and each one carries the rule ID. kube-apiserver is upgraded before components that must not be newer than it. Observed removed-API usage is migrated before the minor that removes it.
- Structural edges (`runs-on`, `part-of`, `reports`, `manages`) record what was observed. They do not imply an upgrade order.

CNI, CSI, runtime, CoreDNS, etcd, operators, and Helm releases without a compatibility rule become `verify` steps. They are not inserted into a guessed sequence. Kubelet and kube-proxy upgrades are required only when the next control-plane minor would exceed the published skew. Otherwise aligning them to the target minor is recommended. The plan does not invent patch numbers. A target of `1.33` means minor 1.33, not 1.33.0.

The brief this POC was built from stopped at "Graph requirements." The graph above is the contract implemented here: directed, serializable, and ordered only by verified rules.

## Build

```bash
go test ./...
go build -o bin/clusterpilot ./cmd/clusterpilot
```

Requires Go 1.27.3. An older `go` command downloads that toolchain from the `go` line in `go.mod`.

`make test` runs the unit tests and does not need a cluster. `make test-kind` starts a kind cluster and runs the ten use cases in [test/kind/README.md](test/kind/README.md). `make test-all` runs both. To install the published module:

```bash
go install github.com/deepak-muley/k8s-cluster-pilot/cmd/clusterpilot@latest
```

## Out of scope

ClusterPilot does not simulate an upgrade, score a fleet, or apply changes. It does not shell out to Pluto, kubent, KubePug, or Silver Surfer. The removed-API dataset is the pinned Pluto file described above. Skew and disruption rules still cite the Kubernetes documentation.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Agents working in this repository should read [AGENTS.md](AGENTS.md). This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). Report vulnerabilities privately, as described in [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE). Copyright 2026 deepak-muley.
