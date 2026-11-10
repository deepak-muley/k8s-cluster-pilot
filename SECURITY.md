# Security

ClusterPilot is a local, read-only CLI. It lists Kubernetes objects and Helm release metadata. It does not create, update, patch, or delete objects, and it does not store Secret data, ConfigMap data, environment values, webhook CA bundles, or Helm values.

## Reporting a vulnerability

Report privately through a GitHub security advisory:

https://github.com/deepak-muley/k8s-cluster-pilot/security/advisories/new

Please include the version (`clusterpilot --version`), the command you ran, and what you observed. Do not include a kubeconfig, Secret contents, or a full inventory from a cluster you do not intend to disclose. Maintainers will acknowledge the report and agree on a fix before any public issue or commit describes the details.

## What to report

- Discovery writing to the API server, or requesting Secret data, ConfigMap data, or Helm values
- Credentials, tokens, or key material written into an inventory, plan, log, or error
- A crafted inventory or rule file that causes unexpected code execution

An `unknown` or `compatible` result you disagree with is a product bug. Open a public issue for that, with a small fixture and no live credentials.
