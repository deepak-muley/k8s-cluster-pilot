package knowledge

import "testing"

func TestBuiltinRules(t *testing.T) {
	base, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if base.Meta.RetrievedAt != "2026-11-09" {
		t.Fatalf("metadata %+v", base.Meta)
	}
	removed := base.ByType(RuleRemovedAPI)
	if len(removed) < 30 {
		t.Fatalf("removed API rules = %d", len(removed))
	}
	for _, id := range []string{
		"kubelet-apiserver-skew",
		"kube-apiserver-no-minor-skip",
		"removed-batch-v1beta1-cronjob",
		"removed-flowcontrol-v1beta3",
		"apiserver-admission-webhook-readiness",
	} {
		if _, ok := base.ByID(id); !ok {
			t.Fatalf("missing rule %s", id)
		}
	}
	rule, _ := base.ByID("kubelet-apiserver-skew")
	params, err := rule.Skew()
	if err != nil {
		t.Fatal(err)
	}
	if params.MaxOlderMinor != 3 || params.WhenSubjectMinorBelow == nil || params.WhenSubjectMinorBelow.MaxOlderMinor != 2 {
		t.Fatalf("skew params %+v", params)
	}
}
