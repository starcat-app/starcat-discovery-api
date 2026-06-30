// Package version 暴露构建版本信息。
package version

const (
	// Service 是客户端 ping 和日志中使用的服务名。
	Service = "discovery"
	// Version 是当前服务版本。发布时由 changelog 同步维护。
	Version = "0.1.0"
)
