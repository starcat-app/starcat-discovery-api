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
	if result.Entries[1].TargetType != "external_resource" {
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

func TestParseREADMEHandlesBlockquotesTablesAndRepositoryResources(t *testing.T) {
	// 这些片段分别保留 awesome-java、awesome-design-md 与 awesome-cursorrules
	// 的真实结构特征，防止解析器再次只兼容 Markdown list。
	readme := []byte(`# Catalog

<details>
<summary>Libraries</summary>

> **[ArchUnit](https://github.com/TNG/ArchUnit)** - Test architecture rules.

</details>

| Project | Description |
| --- | --- |
| [DesignMD](https://getdesign.md/example/design-md) | Design resource |

- [Cursor rule](https://github.com/PatrickJS/awesome-cursorrules/blob/main/rules/react.mdc) - React rule.
`)
	result, err := ParseREADME(
		readme,
		"PatrickJS/awesome-cursorrules",
		"https://github.com/PatrickJS/awesome-cursorrules/blob/main/README.md",
		"sha-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(result.Entries); got != 3 {
		t.Fatalf("entries = %d, want 3: %#v", got, result.Entries)
	}
	if got := result.Entries[0].TargetType; got != "github_repo" {
		t.Fatalf("blockquote target = %q", got)
	}
	if got := result.Entries[0].EntryDescription; got != "Test architecture rules." {
		t.Fatalf("blockquote description = %q", got)
	}
	if got := result.Entries[1].TargetType; got != "external_resource" {
		t.Fatalf("table target = %q", got)
	}
	if got := result.Entries[2].TargetType; got != "repository_resource" {
		t.Fatalf("repository resource target = %q", got)
	}
	if result.ExtractedCount != 3 || result.InvalidCount != 0 {
		t.Fatalf("diagnostics = %#v", result)
	}
}

func TestNormalizeTargetMapsOtherRepositoryDeepLinksToRepositoryRoot(t *testing.T) {
	target, err := NormalizeTargetForSource(
		"https://github.com/TNG/ArchUnit/blob/main/README.md#usage",
		"akullpp/awesome-java",
	)
	if err != nil {
		t.Fatal(err)
	}
	if target.Type != "github_repo" || target.RepoFullName != "TNG/ArchUnit" || target.URL != "https://github.com/TNG/ArchUnit" {
		t.Fatalf("unexpected target: %#v", target)
	}
}

func TestParseREADMERecognizesSourceIconsAcrossSourcesAndContainers(t *testing.T) {
	// 保留 awesome-mac 的真实链接结构，但同时用其他来源和容器验证通用规则，
	// 防止把修复绑定到某个仓库名，或把图标 alt 当成项目名称。
	entry := `[Starcat](https://starcat.ink/) - Native, local-first GitHub Stars manager. [![Open-Source Software][OSS Icon]](https://github.com/starcat-app/Starcat) ![Freeware][Freeware Icon] [![App Store][app-store Icon]](https://apps.apple.com/app/id6788809803)`
	for _, sourceRepo := range []string{"jaywcjlove/awesome-mac", "acme/awesome-tools"} {
		for _, container := range []struct {
			name string
			body string
		}{
			{name: "list", body: "* " + entry},
			{name: "blockquote", body: "> " + entry},
			{name: "table", body: "| Project |\n| --- |\n| " + entry + " |"},
		} {
			t.Run(sourceRepo+"/"+container.name, func(t *testing.T) {
				markdown := []byte("## Developer Tools\n\n" + container.body + "\n\n[OSS Icon]: https://example.com/oss.svg\n[Freeware Icon]: https://example.com/free.svg\n[app-store Icon]: https://example.com/store.svg\n")
				result, err := ParseREADME(markdown, sourceRepo, "https://github.com/"+sourceRepo+"/blob/main/README.md", "sha-icons")
				if err != nil {
					t.Fatal(err)
				}
				entries, _ := entriesForProfile("generic", result.Entries, result.IgnoredCount)
				if len(entries) != 1 {
					t.Fatalf("repository entries = %+v, want Starcat", entries)
				}
				got := entries[0]
				if got.TargetKey != "github_name:starcat-app/starcat" || got.EntryTitle != "Starcat" ||
					got.EntryDescription != "Native, local-first GitHub Stars manager." ||
					got.RawURL != "https://github.com/starcat-app/Starcat" ||
					got.SourceAnchorURL != "https://github.com/"+sourceRepo+"/blob/main/README.md#developer-tools" ||
					got.LastSeenSHA != "sha-icons" {
					t.Fatalf("source icon lost project metadata: %+v", got)
				}
			})
		}
	}
}

func TestParseREADMEFiltersBadgesAndDeduplicatesSourceIcons(t *testing.T) {
	markdown := []byte(`## Tools

- [Alpha](https://github.com/example/alpha) - Useful tool. [![Source Code](https://example.com/source.svg)](https://github.com/example/alpha)
- [Beta](https://beta.example) - Another tool. [![Source Code](https://example.com/source.svg)](https://github.com/example/beta)
- [Badges](https://example.com) - Not source links. [![build](https://example.com/build.svg)](https://github.com/example/build) [![App Store](https://example.com/store.svg)](https://github.com/example/store) [![Sponsor](https://example.com/sponsor.svg)](https://github.com/example/sponsor)
- [External](https://external.example) - No repository. [![Source Code](https://example.com/source.svg)](https://other.example/source)
- [![Source Code](https://example.com/source.svg)](https://github.com/example/orphan)
`)
	result, err := ParseREADME(markdown, "acme/awesome", "", "sha-icons")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := entriesForProfile("generic", result.Entries, result.IgnoredCount)
	if len(entries) != 2 || entries[0].EntryTitle != "Alpha" || entries[1].EntryTitle != "Beta" {
		t.Fatalf("unexpected badge entries: %+v", entries)
	}
	if entries[1].EntryDescription != "Another tool." || result.DuplicateCount != 1 {
		t.Fatalf("description or duplicate diagnostics = %+v", result)
	}
}
