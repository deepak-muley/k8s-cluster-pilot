# Kind use cases

These scenarios start a real [kind](https://kind.sigs.k8s.io/) cluster, install a small set of fixtures, and run ClusterPilot against that inventory. They show the questions the tool is for. `make test` does not start a cluster. `make test-all` runs the unit tests and then these scenarios.

## Run

You need Docker, `kind`, `kubectl`, and Helm 3.

```bash
make test-kind
```

The cluster is `clusterpilot-usecases` on `kindest/node:v1.31.14`. That version still serves `flowcontrol.apiserver.k8s.io/v1beta3` FlowSchema, which the pinned Pluto file says is removed in Kubernetes 1.32. The rule matches only a user and resource this scenario never sends. The cluster is deleted at the end.

```bash
KEEP_KIND=1 make test-kind          # leave the cluster up
KIND_REUSE=1 make test-kind         # reuse a cluster of the same name when it already exists
KIND_NODE_IMAGE=kindest/node:v1.31.14 make test-kind
```

The node image has to be Kubernetes 1.31. A different minor changes which APIs are still servable.

## What gets installed

| Fixture | Why it is there |
|---|---|
| Deployment `pause` and a PodDisruptionBudget with `minAvailable: 1` | One ready pod means the budget allows zero disruptions. |
| Pod `stuck` with a readiness probe that cannot succeed | The pod is up and not Ready. |
| Helm release `legacy-api` with a `v1beta3` FlowSchema | Helm keeps that apiVersion in the release manifest. `kubectl apply` would rewrite it to `v1`, and discovery would not see the removed API. |
| ValidatingWebhookConfiguration with `failurePolicy: Fail` | It matches Events only, so it does not block the other fixtures. |
| Helm release `nats` chart `0.1.0`, app `2.10`, no `kubeVersion` | An app version with no compatibility rule. |
| Helm release `platform` with `kubeVersion: >=1.25.0-0 <1.33.0-0` | A publisher constraint. It includes 1.31 and excludes 1.33. |

kind also brings kindnet, CoreDNS, etcd, containerd, and the local-path provisioner. No rule covers those.

## The ten cases

1. **Control-plane skew can be a real compatible result.** The kubelet and kube-apiserver on this node are the same minor, and the skew rule says that is allowed. Target `1.31`.

2. **The current minor does not need an upgrade plan.** Target `1.31` records that no minor hop is required and does not add a kube-apiserver upgrade step.

3. **A removed API that is still in use blocks the target.** Target `1.32` is `blocked` because the legacy FlowSchema is removed in that minor.

4. **A two-minor jump is a sequence, and the patch is not invented.** Target `1.33` migrates that FlowSchema before the 1.32 hop, then hops to 1.33. The last hop stops at minor `1.33`.

5. **Deprecation before the removal minor is a risk, not a block.** On target `1.31` the same FlowSchema is already deprecated and is not removed yet. The finding is `potential-risk` and does not block the plan.

6. **A chart `kubeVersion` is the publisher's claim.** Target `1.33` makes `platform` a `potential-risk` because the constraint excludes 1.33. Target `1.31` leaves it `unknown`, including when the constraint allows 1.31.

7. **Apps and addons without a rule stay unknown and unordered.** `nats`, kindnet, CoreDNS, etcd, and containerd are `unknown` verify steps. The plan does not put an `upgrade-before` edge on them. Helm values and ConfigMap data are not copied into the inventory.

8. **A drain can be blocked when the versions are fine.** The pause budget allows zero disruptions. The stuck pod is reported as health, and the detail says it is not a version result.

9. **Admission webhooks are not assumed to work on the next apiserver.** The failing webhook is a `potential-risk` and a verify step.

10. **A downgrade is refused.** Target `1.30` is `blocked`. No control-plane upgrade step is created for it.
