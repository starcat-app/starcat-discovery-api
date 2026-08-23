// Package awesome implements README parsing and synchronization for managed Awesome sources.
package awesome

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var repoPartPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var reservedGitHubOwners = map[string]struct{}{
	"features": {}, "marketplace": {}, "orgs": {}, "settings": {}, "sponsors": {}, "topics": {},
}

// NormalizedTarget is a safe, canonical classification of one README link.
type NormalizedTarget struct {
	Type         string
	Key          string
	RepoFullName string
	URL          string
}

// NormalizeTarget classifies an HTTP(S) README link without guessing an external product's repo.
func NormalizeTarget(raw string) (NormalizedTarget, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return NormalizedTarget{}, fmt.Errorf("invalid absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return NormalizedTarget{}, fmt.Errorf("unsupported URL scheme")
	}
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""

	if parsed.Host == "github.com" || parsed.Host == "www.github.com" {
		parts := splitPath(parsed.Path)
		if len(parts) != 2 {
			return NormalizedTarget{}, fmt.Errorf("GitHub URL is not a repository root")
		}
		owner := parts[0]
		repo := strings.TrimSuffix(parts[1], ".git")
		if !validRepoPart(owner) || !validRepoPart(repo) {
			return NormalizedTarget{}, fmt.Errorf("invalid GitHub repository path")
		}
		if _, reserved := reservedGitHubOwners[strings.ToLower(owner)]; reserved {
			return NormalizedTarget{}, fmt.Errorf("GitHub URL is not a repository")
		}
		fullName := owner + "/" + repo
		canonical := "https://github.com/" + fullName
		return NormalizedTarget{Type: "github_repo", Key: "github_name:" + strings.ToLower(fullName), RepoFullName: fullName, URL: canonical}, nil
	}

	parsed.Path = cleanExternalPath(parsed.Path)
	return NormalizedTarget{Type: "external", Key: "external:" + parsed.String(), URL: parsed.String()}, nil
}

// NormalizeSourceInput accepts the two explicit custom-source input forms from the product contract.
func NormalizeSourceInput(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "git@github.com:") {
		trimmed = strings.TrimPrefix(trimmed, "git@github.com:")
		trimmed = strings.TrimSuffix(trimmed, ".git")
	}
	if !strings.Contains(trimmed, "://") {
		parts := splitPath(trimmed)
		if len(parts) != 2 || !validRepoPart(parts[0]) || !validRepoPart(strings.TrimSuffix(parts[1], ".git")) {
			return "", fmt.Errorf("source must be owner/repo or a GitHub repository URL")
		}
		return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"), nil
	}
	target, err := NormalizeTarget(trimmed)
	if err != nil || target.Type != "github_repo" {
		return "", fmt.Errorf("source must be a GitHub repository root")
	}
	return target.RepoFullName, nil
}

func splitPath(value string) []string {
	pieces := strings.Split(strings.Trim(value, "/"), "/")
	result := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		if piece != "" {
			result = append(result, piece)
		}
	}
	return result
}

func validRepoPart(value string) bool {
	return value != "" && value != "." && value != ".." && repoPartPattern.MatchString(value)
}

func cleanExternalPath(value string) string {
	cleaned := path.Clean("/" + strings.TrimSpace(value))
	if cleaned == "/." {
		return "/"
	}
	return cleaned
}
