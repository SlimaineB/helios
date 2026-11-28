package gitops

import (
	"strings"
	"testing"
)

func TestDecodeDocumentsAndTypedSpecs(t *testing.T) {
	raw := []byte(`
apiVersion: helios.dev/v1alpha1
kind: Project
metadata:
  name: team
spec:
  repoPatterns:
    - "https://example.com/*"
---
apiVersion: helios.dev/v1alpha1
kind: Repository
metadata:
  name: app-repo
spec:
  url: https://example.com/app.git
---
apiVersion: helios.dev/v1alpha1
kind: Application
metadata:
  name: demo
  project: team
spec:
  repoRef: app-repo
  branch: main
  path: ansible/site.yml
  vars:
    env: prod
    replicas: 2
  autoHeal: true
  syncInterval: 30
  executionMode: runner
  runnerTags: [eu, web]
  inventorySource: hub
  inventory:
    groups:
      - name: web
        hosts:
          - name: web-1
---
apiVersion: helios.dev/v1alpha1
kind: Assignment
metadata:
  name: demo-on-host
spec:
  application: demo
  instance: ins-1
  autoHeal: false
`)
	docs, err := DecodeDocuments(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 4 {
		t.Fatalf("got %d docs, want 4", len(docs))
	}
	for _, doc := range docs {
		if err := doc.Validate(); err != nil {
			t.Fatalf("%s: %v", doc.Kind, err)
		}
	}

	project, err := docs[0].Project()
	if err != nil {
		t.Fatal(err)
	}
	if project.RepoPatterns != "https://example.com/*" {
		t.Fatalf("repoPatterns = %q", project.RepoPatterns)
	}

	app, err := docs[2].Application()
	if err != nil {
		t.Fatal(err)
	}
	if app.Project != "team" || app.RepoRef != "app-repo" || !app.AutoHeal || app.SyncInterval != 30 {
		t.Fatalf("unexpected app spec: %+v", app)
	}
	if app.Vars != `{"env":"prod","replicas":2}` {
		t.Fatalf("vars = %s", app.Vars)
	}
	if app.ExecutionMode != "runner" || app.InventorySource != "hub" ||
		len(app.RunnerTags) != 2 || app.RunnerTags[0] != "eu" || app.RunnerTags[1] != "web" {
		t.Fatalf("unexpected runner app spec: %+v", app)
	}
	if !strings.Contains(app.InventoryData, "web-1") {
		t.Fatalf("inventory data = %s", app.InventoryData)
	}

	assignment, err := docs[3].Assignment()
	if err != nil {
		t.Fatal(err)
	}
	if assignment.Application != "demo" || assignment.Instance != "ins-1" || assignment.AutoHeal == nil || *assignment.AutoHeal {
		t.Fatalf("unexpected assignment spec: %+v", assignment)
	}
}

func TestDocumentValidateRejectsUnknownKind(t *testing.T) {
	doc := Document{Kind: "Widget", Metadata: Metadata{Name: "x"}}
	if err := doc.Validate(); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestParseDirRejectsEscape(t *testing.T) {
	if _, err := ParseDir(t.TempDir(), "../outside"); err == nil {
		t.Fatal("expected path escape error")
	}
}
