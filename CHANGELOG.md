# Changelog

## [v0.6.0] — proposed

尚未打 tag。模块 tag `v0.6.0` 与发布 tag `x-go-v2026.10.2` 计划打在同一个 main 合并提交上，且仅在合并之后、并得到所有者另行批准后创建。

### Breaking

- 模块路径从 `github.com/zanehu-ai/synapse-go` 改为 `github.com/zanehu-ai/x-go`。
  `v0.6.0` 是第一个声明新路径的 tag。`v0.5.0` 及更早版本仍然只能按旧路径导入：`github.com/zanehu-ai/synapse-go@v0.5.0`。
  调用方迁移：`go get github.com/zanehu-ai/x-go@v0.6.0`，把 import 从 `github.com/zanehu-ai/synapse-go/...` 改成 `github.com/zanehu-ai/x-go/...`，然后 `go mod tidy`。不要添加 `replace`。
