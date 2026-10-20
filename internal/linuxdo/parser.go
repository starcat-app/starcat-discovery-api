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

var githubURL = regexp.MustCompile(`(?i)https?://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[^\s<>"']*)?`)
var promotionCue = regexp.MustCompile(`(?i)(源码|源代码|项目地址|开源地址|仓库地址|项目链接|本项目|我的项目|(?:^|[^a-z])(github|repository|repo|source code|地址)\s*[:：])`)
var dependencyCue = regexp.MustCompile(`(?i)(依赖|基于|参考|对比|致谢|竞品|模板来源|(?:^|[^a-z])(dependenc(y|ies)|based on|built with|powered by|inspired by|alternative to|thanks to|references?|comparison)(?:[^a-z]|$))`)
var supportingCue = regexp.MustCompile(`(?i)(通过|借助|搭配|依托|(?:^|[^a-z])(using|uses)(?:[^a-z]|$))`)

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
			Links []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:"link"`
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
	// encoding/xml 的无命名空间字段也会匹配 atom:link；Discourse 把 Atom 元数据
	// 放在 RSS link 后面时，会把 string 字段覆盖为空。只认可唯一的无命名空间
	// RSS link，既保留真实频道地址，也不让其它命名空间冒充来源。
	channelLink, channelLinks := "", 0
	for _, link := range feed.Channel.Links {
		if link.XMLName.Space == "" {
			channelLink = strings.TrimSpace(link.Value)
			channelLinks++
		}
	}
	channel, err := url.Parse(channelLink)
	if channelLinks != 1 || err != nil || channel.Host != "linux.do" || channel.Scheme != "https" || !validTagPath(channel.Path) || len(feed.Channel.Items) == 0 || len(feed.Channel.Items) > 100 {
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
		candidates, err := extractCandidates(item.Title, item.Description)
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

// extractCandidates 只提取帖子自身的证据；预览卡片取其目标而非复制的 README 链接。
// 主项目按标题和链接局部语境自动判断，不把“缺少某个关键词”变成人工审批任务。
func extractCandidates(title, raw string) ([]model.LinuxDoCandidate, error) {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	result := []model.LinuxDoCandidate{}
	indices := map[string]int{}
	add := func(fullName, context string) {
		if index, exists := indices[fullName]; exists {
			// 重复链接不增加置信度；保留负面角色证据，不能让后来的下载链接洗掉依赖语境。
			old := result[index].Context
			if dependencyCue.MatchString(context) || (!dependencyCue.MatchString(old) && promotionCue.MatchString(context)) {
				result[index].Context = boundedContext(context + "\n" + old)
			}
			return
		}
		indices[fullName] = len(result)
		result = append(result, model.LinuxDoCandidate{FullName: fullName, Context: boundedContext(context)})
	}
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
				if a.Key == "class" && strings.Contains(a.Val, "quote") {
					return nil
				}
			}
			if n.Data != "a" && hasClass(n, "onebox") {
				if fullName := previewRepository(n); fullName != "" {
					add(fullName, sectionContext(n)+" "+fullName)
				}
				return nil
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
					add(fullName, linkContext(n, nodeText(n, 100)))
				}
				return nil
			}
		}
		if n.Type == html.TextNode {
			previous := 0
			for _, match := range githubURL.FindAllStringIndex(n.Data, -1) {
				rawURL := strings.TrimRight(n.Data[match[0]:match[1]], ".,;:!?)]}，。；：！？）】》")
				if fullName, ok := CanonicalRepository(rawURL); ok {
					add(fullName, linkContext(n, n.Data[previous:match[0]]+rawURL))
				}
				previous = match[1]
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
	classifyCandidates(title, result)
	return result, nil
}

// classifyCandidates 也用于旧候选升级重判；证据不足时自动跳过，只有网络失败才等待重试。
func classifyCandidates(title string, candidates []model.LinuxDoCandidate) {
	for i := range candidates {
		c := &candidates[i]
		c.RuleVersion = model.LinuxDoRuleVersion
		c.ExplicitPromotion = false
		switch {
		case dependencyCue.MatchString(c.Context):
			c.Reason = "automatically skipped: dependency, reference or comparison"
		case promotionCue.MatchString(c.Context):
			c.ExplicitPromotion, c.Reason = true, "project link context"
		case projectNameInTitle(title, c.FullName):
			c.ExplicitPromotion, c.Reason = true, "project name matches topic title"
		case supportingCue.MatchString(c.Context):
			c.Reason = "automatically skipped: supporting tool without project attribution"
		case len(candidates) == 1:
			c.ExplicitPromotion, c.Reason = true, "single repository in promotion topic"
		default:
			c.Reason = "automatically skipped: multiple repositories without project attribution"
		}
	}
}

// 标题允许大小写、连字符和空格差异；通用仓库名不能仅靠标题命中就获得推广身份。
func projectNameInTitle(title, fullName string) bool {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return false
	}
	words := strings.FieldsFunc(strings.ToLower(parts[1]), func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	name := strings.Join(words, "")
	switch name {
	case "app", "api", "repo", "project", "tool", "server", "client", "frontend", "backend", "community", "docs", "demo", "template":
		return false
	}
	if len(name) < 3 {
		return false
	}
	for i := range words {
		words[i] = regexp.QuoteMeta(words[i])
	}
	// 英文单词边界避免 foo 命中 foobar，同时允许中文标题及 slug 的分隔符差异。
	pattern, err := regexp.Compile(`(?i)(?:^|[^a-z0-9])` + strings.Join(words, `[\s._-]*`) + `(?:[^a-z0-9]|$)`)
	return err == nil && pattern.MatchString(title)
}

func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			for _, value := range strings.Fields(attr.Val) {
				if value == class {
					return true
				}
			}
		}
	}
	return false
}

// Onebox 的目标 URL 优先于卡片内的其它链接；没有元数据时才使用首个仓库标题链接。
func previewRepository(n *html.Node) string {
	for _, attr := range n.Attr {
		if attr.Key == "data-onebox-src" {
			if fullName, ok := CanonicalRepository(attr.Val); ok {
				return fullName
			}
			return ""
		}
	}
	var headingRepository func(*html.Node, int) string
	headingRepository = func(node *html.Node, depth int) string {
		if depth > 128 {
			return ""
		}
		if node.Type == html.ElementNode && strings.Contains("|h1|h2|h3|h4|h5|h6|", "|"+node.Data+"|") {
			return nodeRepository(node, 0)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if name := headingRepository(child, depth+1); name != "" {
				return name
			}
		}
		return ""
	}
	return headingRepository(n, 0)
}

func nodeRepository(n *html.Node, depth int) string {
	if depth > 128 {
		return ""
	}
	if n.Type == html.ElementNode && n.Data == "a" {
		for _, attr := range n.Attr {
			if attr.Key == "href" {
				if name, ok := CanonicalRepository(attr.Val); ok {
					return name
				}
			}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if name := nodeRepository(child, depth+1); name != "" {
			return name
		}
	}
	return ""
}

// 同段有多个链接时，只取当前链接之前的局部说明，防止“基于”的依赖被“项目地址”误收。
func linkContext(n *html.Node, label string) string {
	current, prefix := n, ""
	for depth := 0; current.Parent != nil && depth < 8; depth++ {
		for sibling := current.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
			if nodeRepository(sibling, 0) != "" {
				if strings.TrimSpace(prefix) == "" {
					for first := current.Parent.FirstChild; first != nil && nodeRepository(first, 0) == ""; first = first.NextSibling {
						prefix += nodeText(first, 100) + " "
					}
				}
				break
			}
			prefix = nodeText(sibling, 160) + " " + prefix
			if len(prefix) > 1600 {
				break
			}
		}
		current = current.Parent
		if strings.Contains("|p|li|div|td|th|section|article|aside|body|html|", "|"+current.Data+"|") {
			break
		}
	}
	return boundedContext(sectionContext(current) + " " + prefix + " " + label)
}

// 只继承相邻短说明或最近小标题，不把整篇正文的关键词扩散到每个仓库。
func sectionContext(n *html.Node) string {
	for depth := 0; n != nil && depth < 8; depth++ {
		for sibling, count := n.PrevSibling, 0; sibling != nil && count < 12; sibling, count = sibling.PrevSibling, count+1 {
			if sibling.Type != html.ElementNode {
				continue
			}
			text := nodeText(sibling, 120)
			if strings.Contains("|h1|h2|h3|h4|h5|h6|", "|"+sibling.Data+"|") {
				return text
			}
			if nodeRepository(sibling, 0) == "" && len([]rune(text)) < 100 && (promotionCue.MatchString(text) || dependencyCue.MatchString(text)) {
				return text
			}
		}
		n = n.Parent
	}
	return ""
}

func boundedContext(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > 400 {
		runes = runes[:400]
	}
	return string(runes)
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
