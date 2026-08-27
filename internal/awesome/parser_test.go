package awesome

import (
	"testing"
)

func TestParseREADMEUsesASTSectionsAndFiltersNonRepos(t *testing.T) {
	markdown := []byte(`# Awesome Test

[![build](https://img.shields.io/badge/build-ok.svg)](https://example.com/build)

## Utilities

- [Alpha](https://github.com/Example/Alpha) - Fast transfer tool.
- [Issues](https://github.com/Example/Alpha/issues) - must be rejected.
- [Website](https://example.com/product) - external product.
- [Self](https://github.com/acme/awesome-test) - source itself.

Nested
------

- [Beta][beta] — A nested reference project.
  - [Gamma](https://github.com/example/gamma) child project.
- [Alpha again](https://github.com/example/alpha) duplicate.

[beta]: https://github.com/example/beta
`)
	result, err := ParseREADME(markdown, "acme/awesome-test", "https://github.com/acme/awesome-test/blob/main/README.md", "sha-1")
	if err != nil {
		t.Fatalf("ParseREADME() error = %v", err)
	}
	if len(result.Entries) != 4 {
		t.Fatalf("entries = %d, want 4: %+v", len(result.Entries), result.Entries)
	}
	if result.InvalidCount != 1 {
		t.Fatalf("invalid count = %d, want 1", result.InvalidCount)
	}
	if result.DuplicateCount != 1 {
		t.Fatalf("duplicate count = %d, want 1", result.DuplicateCount)
	}
	if got := result.Entries[0].SectionPath; len(got) != 2 || got[1] != "Utilities" {
		t.Fatalf("first section = %#v", got)
	}
	if result.Entries[0].EntryDescription != "Fast transfer tool." {
		t.Fatalf("description = %q", result.Entries[0].EntryDescription)
	}
	if result.Entries[1].TargetType != "external" {
		t.Fatalf("external target = %+v", result.Entries[1])
	}
	if got := result.Entries[2].SectionPath; len(got) != 2 || got[1] != "Nested" {
		t.Fatalf("setext section = %#v", got)
	}
}

func TestNormalizeTargetAcceptsOnlyGitHubRepositoryRoots(t *testing.T) {
	target, err := NormalizeTarget("http://github.com/Owner/Repo.git/")
	if err != nil {
		t.Fatalf("NormalizeTarget() error = %v", err)
	}
	if target.Type != "github_repo" || target.RepoFullName != "Owner/Repo" || target.URL != "https://github.com/Owner/Repo" {
		t.Fatalf("target = %+v", target)
	}
	for _, raw := range []string{
		"https://github.com/owner",
		"https://github.com/owner/repo/issues",
		"https://gist.github.com/owner/id",
	} {
		if target, err := NormalizeTarget(raw); err == nil && target.Type == "github_repo" {
			t.Fatalf("%q unexpectedly normalized as repo: %+v", raw, target)
		}
	}
}

func TestNormalizeSourceInputSupportsExplicitForms(t *testing.T) {
	cases := map[string]string{
		"owner/repo":                     "owner/repo",
		"https://github.com/owner/repo/": "owner/repo",
		"git@github.com:owner/repo.git":  "owner/repo",
	}
	for input, want := range cases {
		got, err := NormalizeSourceInput(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeSourceInput(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}
