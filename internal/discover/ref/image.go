// Package ref parses image and runtime strings without guessing a version
// from a name.
package ref

import (
	"strings"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/semver"
)

// ImageNameAndTag splits a container image reference into repository and tag.
// Digests and untagged references return an empty tag.
func ImageNameAndTag(reference string) (name, tag string) {
	reference = strings.TrimSpace(reference)
	if i := strings.Index(reference, "@"); i >= 0 {
		reference = reference[:i]
	}
	slash := strings.LastIndex(reference, "/")
	colon := strings.LastIndex(reference, ":")
	if colon > slash {
		return reference[:colon], reference[colon+1:]
	}
	return reference, ""
}

// SemverTag returns tag when it is a semantic version.
func SemverTag(tag string) (string, bool) {
	if _, err := semver.Parse(tag); err != nil {
		return "", false
	}
	return tag, true
}

// ParseRuntime splits "containerd://1.7.13" into a runtime name and version.
func ParseRuntime(raw string) (name, version string) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "://"); i >= 0 {
		return raw[:i], raw[i+3:]
	}
	return raw, ""
}

// KnownControlPlane matches a container to a Kubernetes component.
// The boolean is false when the container is not one of the known components.
// Version is empty when the image tag is not a semantic version.
func KnownControlPlane(containerName, image string) (component, version string, ok bool) {
	repo, tag := ImageNameAndTag(image)
	base := repo
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	known := []struct {
		name      string
		component string
	}{
		{"kube-apiserver", "kube-apiserver"},
		{"kube-controller-manager", "kube-controller-manager"},
		{"kube-scheduler", "kube-scheduler"},
		{"cloud-controller-manager", "cloud-controller-manager"},
		{"kube-proxy", "kube-proxy"},
		{"coredns", "coredns"},
		{"etcd", "etcd"},
	}
	for _, item := range known {
		if containerName == item.name || base == item.name || strings.HasSuffix(repo, "/"+item.name) {
			if parsed, ok := SemverTag(tag); ok {
				return item.component, parsed, true
			}
			return item.component, "", true
		}
	}
	return "", "", false
}
