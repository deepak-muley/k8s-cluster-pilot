package helm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestOmitsSecretData(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Secret
metadata:
  name: db
  namespace: app
stringData:
  password: supersecret
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: app
spec:
  template:
    spec:
      containers:
      - name: web
        image: example/web:1.2.3
        env:
        - name: TOKEN
          value: tokensecret
`
	refs := manifestRefs(manifest, false)
	encoded, err := json.Marshal(refs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "supersecret") || strings.Contains(string(encoded), "tokensecret") {
		t.Fatalf("secret material leaked: %s", encoded)
	}
	if len(refs) != 2 || refs[1].Kind != "Deployment" || refs[1].Name != "web" {
		t.Fatalf("refs %+v", refs)
	}
}
