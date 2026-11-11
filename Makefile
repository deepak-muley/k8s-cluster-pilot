.PHONY: build test test-kind test-all fmt update-pluto

build:
	go build -o bin/clusterpilot ./cmd/clusterpilot

test:
	go test ./...

# Docker, kind, kubectl, and Helm 3. See test/kind/README.md.
test-kind:
	go test -tags kind -count=1 -timeout 20m ./test/kind/

test-all: test test-kind

fmt:
	gofmt -w .

# VERSION is a Pluto release tag. Releases are listed at
# https://github.com/FairwindsOps/pluto/releases
update-pluto:
	@test -n "$(VERSION)" || (echo "usage: make update-pluto VERSION=v5.24.5" >&2; echo "releases: https://github.com/FairwindsOps/pluto/releases" >&2; exit 2)
	scripts/update-pluto-versions.sh "$(VERSION)"
