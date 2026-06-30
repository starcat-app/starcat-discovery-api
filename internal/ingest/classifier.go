// Package ingest 实现 GitHub 数据到 Discovery catalog 的转换、识别和排序。
package ingest

import (
	"regexp"
	"sort"
	"strings"

	"github.com/dong4j/starcat-discovery-api/internal/github"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// ClassifyTopics 把 GitHub topics / 描述映射成 Starcat 业务主题。
func ClassifyTopics(repo github.Repository) []string {
	text := strings.ToLower(strings.Join(append(repo.Topics, repo.Description, repo.Name, repo.FullName), " "))
	candidates := map[string][]string{
		"ai":         {"ai", "llm", "agent", "machine-learning", "deep-learning", "neural", "openai", "transformer"},
		"privacy":    {"privacy", "security", "encryption", "cryptography", "password", "vpn", "proxy"},
		"networking": {"network", "http", "dns", "tcp", "proxy", "vpn", "mesh", "server"},
		"media":      {"media", "video", "audio", "music", "image", "photo", "ffmpeg", "player"},
		"social":     {"social", "chat", "community", "mastodon", "matrix", "forum", "messaging"},
		"reading":    {"read", "reader", "ebook", "markdown", "rss", "book", "note"},
		"tools":      {"tool", "cli", "terminal", "developer-tools", "productivity", "automation"},
	}
	return matchCodes(text, candidates)
}

// DetectPlatforms 从 release asset、topics 和语言推断平台。
func DetectPlatforms(repo github.Repository, releases []github.Release) []string {
	textParts := append([]string{}, repo.Topics...)
	textParts = append(textParts, repo.Language, repo.Description, repo.Name, repo.FullName)
	for _, release := range releases {
		for _, asset := range release.Assets {
			textParts = append(textParts, asset.Name)
		}
	}
	text := strings.ToLower(strings.Join(textParts, " "))
	candidates := map[string][]string{
		"macos":   {"macos", "darwin", "osx", ".dmg", ".pkg", "apple-silicon", "x86_64-apple"},
		"ios":     {"ios", "iphone", "ipad", ".ipa", "swiftui"},
		"cli":     {"cli", "terminal", "command-line", "shell", "homebrew", "brew"},
		"web":     {"web", "frontend", "browser", "react", "vue", "nextjs", "typescript", "javascript"},
		"server":  {"server", "backend", "api", "docker", "kubernetes", "database"},
		"android": {"android", ".apk", ".aab", "kotlin"},
		"windows": {"windows", "win32", "win64", ".exe", ".msi", ".msix"},
		"linux":   {"linux", "appimage", ".deb", ".rpm", "x86_64-unknown-linux"},
	}
	codes := matchCodes(text, candidates)
	if len(codes) == 0 {
		switch strings.ToLower(repo.Language) {
		case "go", "rust", "shell":
			codes = append(codes, "cli")
		case "swift", "objective-c":
			codes = append(codes, "macos")
		case "kotlin", "java":
			codes = append(codes, "android")
		case "typescript", "javascript":
			codes = append(codes, "web")
		}
	}
	return uniqueSorted(codes)
}

func matchCodes(text string, candidates map[string][]string) []string {
	var codes []string
	normalizedText := nonAlphaNum.ReplaceAllString(text, " ")
	for code, needles := range candidates {
		for _, needle := range needles {
			normalizedNeedle := nonAlphaNum.ReplaceAllString(strings.ToLower(needle), " ")
			if strings.Contains(normalizedText, normalizedNeedle) || strings.Contains(text, strings.ToLower(needle)) {
				codes = append(codes, code)
				break
			}
		}
	}
	return uniqueSorted(codes)
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
