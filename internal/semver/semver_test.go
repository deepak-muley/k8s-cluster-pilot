package semver

import "testing"

func TestParse(t *testing.T) {
	v, err := Parse("v1.32.2+k3s1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Major != 1 || v.Minor != 32 || v.Patch != 2 || !v.HasPatch || v.Pre != "+k3s1" {
		t.Fatalf("parsed %+v", v)
	}
	minorOnly, err := Parse("1.33")
	if err != nil {
		t.Fatal(err)
	}
	if minorOnly.HasPatch {
		t.Fatal("patch was not in the input")
	}
	if _, err := Parse("latest"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCompareAndLag(t *testing.T) {
	apiserver := MustParse("1.32.2")
	kubelet := MustParse("1.30.1")
	lag, ok := kubelet.MinorLag(apiserver)
	if !ok || lag != 2 {
		t.Fatalf("lag %d ok %v", lag, ok)
	}
	if kubelet.CompareMinor(apiserver) >= 0 {
		t.Fatal("expected kubelet minor to be older")
	}
	if _, ok := MustParse("2.0.0").MinorLag(apiserver); ok {
		t.Fatal("different majors are not a minor lag")
	}
}
