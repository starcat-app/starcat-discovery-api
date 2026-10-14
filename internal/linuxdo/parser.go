// Package linuxdo 将授权可访问的公开标签 RSS 转为待核验候选，不执行帖子中的内容。
package linuxdo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"golang.org/x/net/html"
)

const maxFeedBytes = 4 << 20

var repoPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// CanonicalRepository 只接受 GitHub 仓库及其已知子路径，拒绝主机伪装、组织页和重定向服务。
func CanonicalRepository(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Host, "github.com") {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || !repoPart.MatchString(parts[0]) || !repoPart.MatchString(parts[1]) {
		return "", false
	}
	owner, name := parts[0], strings.TrimSuffix(parts[1], ".git")
	if owner == "." || owner == ".." || name == "" || name == "." || name == ".." {
		return "", false
	}
	switch strings.ToLower(owner) {
	case "topics", "orgs", "users", "settings", "marketplace", "sponsors", "collections", "search", "login":
		return "", false
	}
	if len(parts) > 2 {
		switch parts[2] {
		case "tree", "blob", "releases", "issues", "pull", "pulls", "wiki", "discussions":
		default:
			return "", false
		}
	}
	return strings.ToLower(owner + "/" + name), true
}

// ParseFeed 要求完整、非空、属于指定标签的 RSS。挑战页、截断 XML 与未知结构都不能成为空快照。
func ParseFeed(reader io.Reader) ([]model.LinuxDoObservation, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxFeedBytes+1))
	if err != nil || len(body) > maxFeedBytes {
		return nil, fmt.Errorf("invalid Linux.do feed size")
	}
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Link  string `xml:"link"`
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
				Date        string `xml:"pubDate"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("invalid Linux.do RSS: %w", err)
	}
	channel, err := url.Parse(feed.Channel.Link)
	if err != nil || channel.Host != "linux.do" || channel.Scheme != "https" || !validTagPath(channel.Path) || len(feed.Channel.Items) == 0 || len(feed.Channel.Items) > 100 {
		return nil, fmt.Errorf("unexpected Linux.do tag feed")
	}
	observations := make([]model.LinuxDoObservation, 0, len(feed.Channel.Items))
	seen := map[int64]bool{}
	for _, item := range feed.Channel.Items {
		id, ok := topicID(item.Link)
		if !ok || strings.TrimSpace(item.Title) == "" || len(item.Description) > 1<<20 {
			return nil, fmt.Errorf("invalid Linux.do topic")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		date, err := time.Parse(time.RFC1123Z, item.Date)
		if err != nil {
			date, err = time.Parse(time.RFC1123, item.Date)
		}
		if err != nil {
			return nil, fmt.Errorf("invalid Linux.do publication date")
		}
		digest := sha256.Sum256([]byte(item.Title + "\n" + item.Description))
		hash := hex.EncodeToString(digest[:])
		candidates, err := extractCandidates(item.Description)
		if err != nil {
			return nil, err
		}
		for i := range candidates {
			candidates[i].TopicID = id
			candidates[i].ContentHash = hash
		}
		observations = append(observations, model.LinuxDoObservation{
			Topic:      model.LinuxDoTopic{ID: id, URL: fmt.Sprintf("https://linux.do/t/topic/%d", id), Title: item.Title, PublishedAt: date.UTC().Format(time.RFC3339), ContentHash: hash},
			Candidates: candidates,
		})
	}
	return observations, nil
}

// Discourse 可能使用标签名称或 ID 生成 RSS 的 channel link，只接受已确认的同一标签。
func validTagPath(path string) bool {
	switch path {
	case "/tag/2234-tag/2234", "/tag/开源推广", "/tag/开源推广/2234", "/tags/开源推广":
		return true
	default:
		return false
	}
}

// topicID 接受首帖地址，明确拒绝回复链接；客户端只会收到此处生成的固定 HTTPS 回链。
func topicID(raw string) (int64, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host != "linux.do" || u.Scheme != "https" {
		return 0, false
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) < 2 || p[0] != "t" {
		return 0, false
	}
	i := 1
	if _, err := strconv.ParseInt(p[i], 10, 64); err != nil {
		i++
	}
	if i >= len(p) || len(p) > i+2 || (len(p) == i+2 && p[i+1] != "1") {
		return 0, false
	}
	id, err := strconv.ParseInt(p[i], 10, 64)
	return id, err == nil && id > 0
}

// extractCandidates 使用 HTML 树过滤引用/代码/徽章。明确“源码/项目地址”且仅一个仓库才允许自动核验发布。
// 依赖、对比和多项目内容均进入人工审核；不使用 LLM 猜测作者意图。
func extractCandidates(raw string) ([]model.LinuxDoCandidate, error) {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	result := []model.LinuxDoCandidate{}
	indices := map[string]int{}
	ambiguous := map[string]bool{}
	var walk func(*html.Node, int) error
	walk = func(n *html.Node, depth int) error {
		if depth > 128 {
			return fmt.Errorf("Linux.do HTML nesting too deep")
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "blockquote", "code", "pre", "script", "style":
				return nil
			}
			for _, a := range n.Attr {
				if a.Key == "class" && (strings.Contains(a.Val, "quote") || strings.Contains(a.Val, "onebox")) {
					return nil
				}
			}
			if n.Data == "a" {
				// 纯图片链接通常是 build/license 徽章；不能把父段落中的“源码”误作其角色。
				if nodeText(n, 80) == "" {
					return nil
				}
				var href string
				for _, a := range n.Attr {
					if a.Key == "href" {
						href = a.Val
					}
				}
				if fullName, ok := CanonicalRepository(href); ok {
					context := nodeText(n.Parent, 400)
					lower := strings.ToLower(context)
					positive := strings.Contains(lower, "源码") || strings.Contains(lower, "项目地址") || strings.Contains(lower, "开源地址") || strings.Contains(lower, "source code")
					for _, term := range []string{"依赖", "基于", "参考", "对比", "致谢", "dependency", "dependencies", "based on", "inspired", "alternative"} {
						if strings.Contains(lower, term) {
							positive = false
							ambiguous[fullName] = true
						}
					}
					if index, exists := indices[fullName]; exists {
						result[index].ExplicitPromotion = result[index].ExplicitPromotion || positive
					} else {
						indices[fullName] = len(result)
						result = append(result, model.LinuxDoCandidate{FullName: fullName, Context: context, ExplicitPromotion: positive})
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := walk(c, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, 0); err != nil {
		return nil, err
	}
	if len(result) > 30 {
		return nil, fmt.Errorf("too many repository candidates")
	}
	for i := range result {
		// 同一仓库同时出现于“源码”和“依赖”语境时，也不能靠去重消除角色冲突。
		if len(result) != 1 || ambiguous[result[i].FullName] {
			result[i].ExplicitPromotion = false
		}
	}
	return result, nil
}

func nodeText(n *html.Node, limit int) string {
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n == nil || b.Len() >= limit*4 {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	runes := []rune(strings.Join(strings.Fields(b.String()), " "))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
