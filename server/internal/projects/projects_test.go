package projects

import "testing"

func TestMatchRepo(t *testing.T) {
	pat := "https://github.com/acme/*\nssh://git@gitlab.example.com/infra/*"
	for _, url := range []string{
		"https://github.com/acme/ansible.git",
		"ssh://git@gitlab.example.com/infra/prod.git",
	} {
		if !MatchRepo(pat, url) {
			t.Fatalf("expected %q to be allowed", url)
		}
	}
	for _, url := range []string{
		"https://github.com/evil/repo.git",
		"https://github.com/acme-evil/repo.git",
	} {
		if MatchRepo(pat, url) {
			t.Fatalf("expected %q to be denied", url)
		}
	}
	if !MatchRepo("", "anything") {
		t.Fatal("empty patterns must allow everything")
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"", "finance", "team-1", "a.b_c"} {
		if !ValidName(name) {
			t.Fatalf("expected %q valid", name)
		}
	}
	for _, name := range []string{"bad name", " x", "a,b", "-x"} {
		if ValidName(name) {
			t.Fatalf("expected %q invalid", name)
		}
	}
}
