// Package version 暴露服务标识和构建时注入的版本号。
package version

const Service = "discovery"

// Version 必须保持为变量，发布流水线通过 go build -ldflags -X 注入 Git tag 对应版本。
// 本地直接运行时保留开发版本，避免把某次历史发布号误报为当前构建版本。
var Version = "0.0.0-dev"
