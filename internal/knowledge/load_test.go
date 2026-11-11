package knowledge

import (
	"strings"
	"testing"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/semver"
)

func TestBuiltinRules(t *testing.T) {
	base, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if base.Meta.RetrievedAt != "2026-11-09" {
		t.Fatalf("metadata %+v", base.Meta)
	}
	removed := base.ByType(RuleRemovedAPI)
	if len(removed) < 80 {
		t.Fatalf("removed API rules = %d", len(removed))
	}
	deprecated := base.ByType(RuleDeprecatedAPI)
	if len(deprecated) == 0 {
		t.Fatal("expected Pluto deprecations that have no removal version")
	}
	for _, id := range []string{
		"kubelet-apiserver-skew",
		"kube-apiserver-no-minor-skip",
		"removed-batch-v1beta1-cronjob",
		"removed-flowcontrol-apiserver-k8s-io-v1beta3-flowschema",
		"deprecated-storage-k8s-io-v1beta1-volumeattributesclass",
		"apiserver-admission-webhook-readiness",
	} {
		if _, ok := base.ByID(id); !ok {
			t.Fatalf("missing rule %s", id)
		}
	}
	for _, rule := range append(removed, deprecated...) {
		if strings.Contains(rule.ID, "istio") || strings.Contains(rule.ID, "certmanager") {
			t.Fatalf("product api compared as kubernetes: %s", rule.ID)
		}
		if rule.Source.RetrievedAt == "" || !strings.Contains(rule.Source.URL, "/"+rule.Source.RetrievedAt+"/") {
			t.Fatalf("rule %s does not cite its Pluto release: %+v", rule.ID, rule.Source)
		}
	}
	cronjob, _ := base.ByID("removed-batch-v1beta1-cronjob")
	params, err := cronjob.RemovedAPI()
	if err != nil {
		t.Fatal(err)
	}
	if params.RemovedIn != "v1.25.0" || params.Group != "batch" || params.Version != "v1beta1" {
		t.Fatalf("cronjob params %+v", params)
	}
	before, err := params.AppliesAtTarget(semver.MustParse("1.24"))
	if err != nil || before {
		t.Fatalf("1.24 applies=%v err=%v", before, err)
	}
	atRemoval, err := params.AppliesAtTarget(semver.MustParse("1.25"))
	if err != nil || !atRemoval {
		t.Fatalf("1.25 applies=%v err=%v", atRemoval, err)
	}
	volume, _ := base.ByID("deprecated-storage-k8s-io-v1beta1-volumeattributesclass")
	deprecatedParams, err := volume.RemovedAPI()
	if err != nil {
		t.Fatal(err)
	}
	removedNow, err := deprecatedParams.AppliesAtTarget(semver.MustParse("1.35"))
	if err != nil || removedNow {
		t.Fatal("a deprecation with no removal version was treated as removed")
	}
	notYet, err := deprecatedParams.DeprecatedAtTarget(semver.MustParse("1.33"))
	if err != nil || notYet {
		t.Fatalf("1.33 deprecated=%v err=%v", notYet, err)
	}
	deprecatedNow, err := deprecatedParams.DeprecatedAtTarget(semver.MustParse("1.34"))
	if err != nil || !deprecatedNow {
		t.Fatalf("1.34 deprecated=%v err=%v", deprecatedNow, err)
	}
	rule, _ := base.ByID("kubelet-apiserver-skew")
	skew, err := rule.Skew()
	if err != nil {
		t.Fatal(err)
	}
	if skew.MaxOlderMinor != 3 || skew.WhenSubjectMinorBelow == nil || skew.WhenSubjectMinorBelow.MaxOlderMinor != 2 {
		t.Fatalf("skew params %+v", skew)
	}
}
