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

Builtin rules live in `knowledge/rules/` and are embedded in the binary. They were retrieved on 2026-11-09 from:

- [Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/)
- [Deprecated API Migration Guide](https://kubernetes.io/docs/reference/using-api/deprecation-guide/)
- [Disruptions](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/)

The skew rules encode the published minor limits, including the smaller limit for kubelet and kube-proxy versions below 1.25. Removed-API rules cover the guide through the Kubernetes 1.32 removals. The guide retrieved on that date did not list removals for 1.33 or later. Absence of a removal rule is not proof that an API remains.

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

Requires Go 1.24.

## Out of scope

The POC does not simulate an upgrade, score a fleet, or apply changes. It does not implement the other product ideas in the brief: flight recorder, event delivery, supply-chain intelligence, inference fleet management, or telemetry optimization. It also does not shell out to Pluto, kubent, KubePug, or Silver Surfer. Those tools remain complementary. This knowledge base cites the Kubernetes documentation directly instead of copying their datasets.

License: Apache-2.0.
