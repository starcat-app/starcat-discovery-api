package awesome

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

const (
	maxReadmeBytes = 2 * 1024 * 1024
	maxASTNodes    = 200_000
	maxEntries     = 10_000
)

var descriptionPrefixPattern = regexp.MustCompile(`^[\s\-–—:：|]+`)

// ParseResult contains both publishable GitHub candidates and external audit facts.
type ParseResult struct {
	Entries        []model.AwesomeEntry
	InvalidCount   int
	DuplicateCount int
}

// ParseREADME parses list links from a bounded CommonMark/GFM AST.
//
// The AST boundary is intentional: regex-scanning the whole README would mistake badges,
// prose links and table-of-contents anchors for project entries.
func ParseREADME(source []byte, sourceRepo, readmeURL, readmeSHA string) (ParseResult, error) {
	if len(source) == 0 {
		return ParseResult{}, errors.New("README is empty")
	}
	if len(source) > maxReadmeBytes {
		return ParseResult{}, fmt.Errorf("README exceeds %d bytes", maxReadmeBytes)
	}
	markdown := goldmark.New(goldmark.WithExtensions(extension.GFM))
	document := markdown.Parser().Parse(text.NewReader(source))
	section := make([]string, 0, 6)
	result := ParseResult{Entries: make([]model.AwesomeEntry, 0)}
	seen := make(map[string]struct{})
	nodeCount := 0
	entryOrder := 0

	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		nodeCount++
		if nodeCount > maxASTNodes {
			return ast.WalkStop, fmt.Errorf("README AST exceeds %d nodes", maxASTNodes)
		}
		if heading, ok := node.(*ast.Heading); ok {
			section = updateSection(section, heading.Level, cleanText(string(heading.Text(source))))
			return ast.WalkContinue, nil
		}
		item, ok := node.(*ast.ListItem)
		if !ok {
			return ast.WalkContinue, nil
		}

		links := directListItemLinks(item)
		for _, link := range links {
			if hasImageDescendant(link) {
				continue
			}
			rawURL := strings.TrimSpace(string(link.Destination))
			if shouldIgnoreLink(rawURL) {
				continue
			}
			target, normalizeErr := NormalizeTarget(rawURL)
			if normalizeErr != nil {
				result.InvalidCount++
				continue
			}
			if target.Type == "github_repo" && strings.EqualFold(target.RepoFullName, sourceRepo) {
				continue
			}
			if _, exists := seen[target.Key]; exists {
				result.DuplicateCount++
				continue
			}
			seen[target.Key] = struct{}{}
			entryOrder++
			if entryOrder > maxEntries {
				return ast.WalkStop, fmt.Errorf("README exceeds %d entries", maxEntries)
			}

			title := cleanText(string(link.Text(source)))
			if title == "" && target.RepoFullName != "" {
				parts := strings.Split(target.RepoFullName, "/")
				title = parts[len(parts)-1]
			}
			result.Entries = append(result.Entries, model.AwesomeEntry{
				TargetType:       target.Type,
				TargetKey:        target.Key,
				EntryTitle:       title,
				EntryDescription: listItemDescription(item, link, source),
				SectionPath:      append([]string(nil), section...),
				RawURL:           target.URL,
				SourceAnchorURL:  sourceAnchorURL(readmeURL, section),
				EntryOrder:       entryOrder,
				FirstSeenSHA:     readmeSHA,
				LastSeenSHA:      readmeSHA,
			})
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return ParseResult{}, err
	}
	return result, nil
}

func updateSection(current []string, level int, title string) []string {
	if title == "" || level <= 0 {
		return current
	}
	index := level - 1
	if index < len(current) {
		current = current[:index]
	}
	for len(current) < index {
		current = append(current, "")
	}
	return append(current, title)
}

func directListItemLinks(item *ast.ListItem) []*ast.Link {
	links := make([]*ast.Link, 0, 1)
	_ = ast.Walk(item, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node == item {
			return ast.WalkContinue, nil
		}
		if nested, ok := node.(*ast.ListItem); ok && nested != item {
			return ast.WalkSkipChildren, nil
		}
		if link, ok := node.(*ast.Link); ok {
			links = append(links, link)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return links
}

func hasImageDescendant(node ast.Node) bool {
	found := false
	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if _, ok := child.(*ast.Image); ok {
				found = true
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})
	return found
}

func listItemDescription(item *ast.ListItem, primary *ast.Link, source []byte) string {
	all := directListItemText(item, source)
	label := cleanText(string(primary.Text(source)))
	if label != "" {
		all = strings.TrimSpace(strings.TrimPrefix(all, label))
	}
	return descriptionPrefixPattern.ReplaceAllString(all, "")
}

func directListItemText(item *ast.ListItem, source []byte) string {
	parts := make([]string, 0, 8)
	_ = ast.Walk(item, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node == item {
			return ast.WalkContinue, nil
		}
		if nested, ok := node.(*ast.ListItem); ok && nested != item {
			return ast.WalkSkipChildren, nil
		}
		if _, ok := node.(*ast.Image); ok {
			return ast.WalkSkipChildren, nil
		}
		if node.Type() == ast.TypeInline && !node.HasChildren() {
			if value := cleanText(string(node.Text(source))); value != "" {
				parts = append(parts, value)
			}
		}
		return ast.WalkContinue, nil
	})
	return cleanText(strings.Join(parts, " "))
}

func shouldIgnoreLink(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return true
	}
	host := strings.ToLower(parsed.Host)
	return strings.Contains(host, "shields.io") || strings.Contains(host, "badge") ||
		strings.Contains(host, "opencollective.com") || strings.Contains(host, "patreon.com") ||
		strings.Contains(host, "github.com/sponsors")
}

func sourceAnchorURL(readmeURL string, section []string) string {
	base := strings.TrimSpace(readmeURL)
	if base == "" || len(section) == 0 {
		return base
	}
	slug := githubHeadingSlug(section[len(section)-1])
	if slug == "" {
		return base
	}
	return strings.Split(base, "#")[0] + "#" + slug
}

func githubHeadingSlug(value string) string {
	var builder strings.Builder
	previousDash := false
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			previousDash = false
		case unicode.IsSpace(r) || r == '-':
			if builder.Len() > 0 && !previousDash {
				builder.WriteByte('-')
				previousDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
