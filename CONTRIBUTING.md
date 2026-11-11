# Contributing

Thanks for helping with ClusterPilot. This project is released under the Apache License 2.0. By contributing, you agree that your contributions are licensed under that license. See [LICENSE](LICENSE).

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## What belongs here

ClusterPilot is a read-only Kubernetes upgrade intelligence CLI. A result is `compatible` only when a loaded rule says so. A missing rule stays `unknown`. Contributions that treat missing evidence as compatibility will not be accepted.

In scope:

- Discovery that lists or gets objects and does not create, update, patch, or delete them
- Inventory, analysis, plan, and report behavior covered by tests
- Builtin rules copied from a cited upstream document, with the retrieval date

Out of scope: applying an upgrade, simulating one, scoring a fleet, or shelling out to Pluto, kubent, KubePug, or Silver Surfer. Removed Kubernetes APIs are read from the pinned Pluto file, not from a live Pluto process.

## Setup

Go 1.27.3 or newer. `go.mod` sets that minimum. A `go` binary older than 1.27.3 downloads the toolchain when you build or test this module. Replacing `/usr/local/go` is only needed when you want `go version` itself to report 1.27.3 or newer. The current stable patch may be newer than 1.27.3; a newer 1.27 patch satisfies this module. Installers and checksums are at <https://go.dev/dl/>.

```bash
go test ./...
go build -o bin/clusterpilot ./cmd/clusterpilot
```

`make test`, `make build`, and `make fmt` do the same thing. `make fmt` runs `gofmt -w .`.

## Pull requests

- Run `go test ./...` before opening the pull request. CI runs the same command.
- Format Go code with `gofmt`.
- Keep the change focused. Describe why the behavior changed in the pull request.
- Add or update a test when the change affects discovery, analysis, planning, or report text.
- Do not commit `bin/`, `inventory.json`, or `plan.json`. Those paths are gitignored because inventories can describe a real cluster.

## Knowledge base rules

Skew and operations rules live in `knowledge/rules/` and are embedded in the binary. A new or changed rule needs:

- a stable `id`
- a `source.url` for the document the rule was taken from
- `retrievedAt` on that source
- a test that fails if the rule is dropped or inverted

Removed and deprecated Kubernetes APIs are not edited in that directory. They come from `knowledge/pluto/versions.yaml`, pinned to one Pluto release by `knowledge/pluto/source.yaml`. Releases are listed at <https://github.com/FairwindsOps/pluto/releases>. Update the pin with `make update-pluto VERSION=v5.24.5`, then run `go test ./...`. Do not change `versions.yaml` by hand. The checksum in `source.yaml` has to match the file.

Do not invent a compatibility result for CNI, CSI, runtime, CoreDNS, etcd, or a Helm release. Without a loaded rule, those stay `verify` steps and `unknown` findings. A chart `kubeVersion` constraint is a publisher claim: a constraint that includes the target stays `unknown`, and a constraint that excludes it is `potential-risk`.

## Reporting issues

Use a GitHub issue for bugs and feature requests. Leave cluster inventories, kubeconfigs, and Secret data out of the issue. A trimmed fixture that reproduces the analysis is enough.

Report vulnerabilities through a private advisory, as described in [SECURITY.md](SECURITY.md).
