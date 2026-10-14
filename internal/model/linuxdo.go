// Linux.do 的公共来源事实与用户收藏、普通 Discovery 目录分别存储。
package model

import "time"

// LinuxDoTopic 只保存公开首帖的摘要与回链，不复制帖子全文或用户身份。
type LinuxDoTopic struct {
	ID          int64  `json:"id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	PublishedAt string `json:"published_at"`
	VerifiedAt  string `json:"verified_at,omitempty"`
	ContentHash string `json:"-"`
}

// LinuxDoCandidate 是持久化审核队列；证据版本防止旧页面批准了已编辑的新内容。
type LinuxDoCandidate struct {
	ID                int64  `json:"id"`
	TopicID           int64  `json:"topic_id"`
	FullName          string `json:"full_name"`
	ContentHash       string `json:"content_hash"`
	Context           string `json:"context"`
	ExplicitPromotion bool   `json:"explicit_promotion"`
	State             string `json:"state"`
	Reason            string `json:"reason"`
	RepoID            int64  `json:"repo_id"`
	License           string `json:"license"`
}

// LinuxDoObservation 是一次 RSS 中实际看到的首帖；缺席不代表删除。
type LinuxDoObservation struct {
	Topic      LinuxDoTopic
	Candidates []LinuxDoCandidate
}

// LinuxDoOrigin 用数字 ID 关联跨分类来源，不要求客户端已经收藏仓库。
type LinuxDoOrigin struct {
	RepoID     int64  `json:"repo_id"`
	TopicCount int    `json:"topic_count"`
	VerifiedAt string `json:"verified_at"`
}

// LinuxDoSnapshot 是单个 SQLite 事务读取的完整发布快照。
// Revision 在发布、撤回与公开事实变化时单调增加；Complete 区分有效空目录和错误。
type LinuxDoSnapshot struct {
	Revision     int64                    `json:"revision"`
	GeneratedAt  string                   `json:"generated_at"`
	Complete     bool                     `json:"complete"`
	Repositories []DiscoveryItem          `json:"repositories"`
	Origins      []LinuxDoOrigin          `json:"origins"`
	Topics       map[int64][]LinuxDoTopic `json:"topics"`
}

// LinuxDoFeedState 的校验头只有在首帖与候选持久化后才能推进。
type LinuxDoFeedState struct {
	ETag         string
	LastModified string
	CheckedAt    time.Time
}
