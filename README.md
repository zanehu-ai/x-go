# x-go

Go 共享基础设施库。为所有 Go 产品提供统一的基础设施层（L1）和平台能力层（L2）。

## Module path change

模块路径已从 `github.com/zanehu-ai/synapse-go` 改为 `github.com/zanehu-ai/x-go`。

`v0.6.0` 是第一个声明新路径的 tag。`v0.5.0` 及更早版本仍然只能按旧路径导入：`github.com/zanehu-ai/synapse-go@v0.5.0`。GitHub 仓库改名重定向不会修正这一点：Go 工具链会拒绝 go.mod 里的 module 路径与请求路径不一致的模块。

调用方在 `v0.6.0` 打上 tag 之后，直接改 require 和 import，不要在 go.mod 里加 `replace`：

```bash
go get github.com/zanehu-ai/x-go@v0.6.0 && find . -name '*.go' -print0 | xargs -0 perl -pi -e 's#github.com/zanehu-ai/synapse-go#github.com/zanehu-ai/x-go#g' && gofmt -w . && go mod tidy
```

`perl -pi` 在 GNU 和 macOS 上写法相同。若用 `sed`，GNU 是 `sed -i`，macOS 是 `sed -i ''`。单个 import 也可以用 `gofmt -w -r '"github.com/zanehu-ai/synapse-go/config" -> "github.com/zanehu-ai/x-go/config"'`，每个包路径写一条。`go mod tidy` 会去掉不再被引用的旧 require；不要加 `replace`。

## 安装

```bash
# 配置私有模块访问（如仓库为 private）
export GOPRIVATE=github.com/zanehu-ai/*

go get github.com/zanehu-ai/x-go@latest
```

## 包列表

### L1 — 基础设施

| 包 | 说明 |
|---|------|
| `config` | 配置结构体（MySQL/Redis/Auth/SMTP）+ `GetEnv()` 环境变量加载 |
| `db` | MySQL 连接池初始化（GORM） |
| `redis` | Redis 连接初始化 |
| `logger` | 结构化日志（zap） |
| `migrate` | 数据库迁移（golang-migrate） |
| `timeutil` | 时间工具函数 |
| `mailer` | 邮件发送（SMTP + NoopMailer） |

### L2 — 平台能力

| 包 | 说明 |
|---|------|
| `resp` | 统一 API 响应格式（Success/Error/SuccessPage + 错误码） |
| `bizerr` | 业务错误类型（BizError + HandleError + 便捷构造函数） |
| `ginutil` | Gin 工具（路由参数解析 + Context Helpers） |
| `middleware` | HTTP 中间件（CORS、RequestID、JWT、平台/租户 token、Feature Gate、租户状态、角色鉴权、Header Secret） |
| `auth` | 平台/租户 JWT claims、签发、解析、step-up token、audience 校验 |
| `rbac` | 权限码精确匹配、通配符匹配与 subject 权限检查 |
| `ratelimit` | 多维限流（RPM/TPM/并发，Redis Lua 脚本） |
| `crypto` | 密码哈希（bcrypt）+ 验证码生成 + 密码强度校验 + TOTP 生成/验证 |
| `lock` | Redis 分布式锁（SetNX + token 所有权 + Lua 原子释放） |
| `audit` | 审计日志（Gin 中间件自动记录 + 事务内手动记录） |
| `idempotent` | 幂等控制（Idempotency-Key 中间件 + Service 层 Check） |
| `idempotency` | 租户/主体/请求作用域幂等键、请求体哈希、持久化记录、响应重放与 GC |
| `webhook` | HMAC 签名、签名头解析与时间窗口校验 |
| `files` | 租户文件对象、上传策略、对象 key 构造与文件类型约束 |
| `notification` | 通知消息结构、类型规范化、payload 校验 |
| `job` | Job lease、retry/backoff policy、cron 表达式防护 |
| `jobpayload` | 后台任务 payload 类型读取工具 |
| `outbox` | Reliable Events outbox 记录、内存 store、GORM 事务 inserter |
| `reliableevents` | 可靠事件 outbox 服务、pending/list、发送状态、失败重试与 consumer 幂等记录 |
| `sequence` | Redis 序列号生成（日期分区 + 自动递增） |
| `notify` | 通知抽象层（邮件/Webhook + 重试包装器） |
| `validate` | 通用校验（邮箱/手机号格式） |
| `healthcheck` | 健康检查（多检查项注册 + Gin Handler） |
| `scheduler` | 定时任务调度（后台 goroutine + 分布式锁） |
| `storage` | 对象存储抽象（本地文件系统 / 可扩展 OSS） |
| `pii` | 多司法辖区 PII 脱敏（邮箱、手机号、证件号、银行卡、JWT/API key、IP） |

### L3 — 架构能力

| 包 | 说明 |
|---|------|
| `circuitbreaker` | 熔断器（Closed/Open/HalfOpen 状态机） |
| `tenant` | 多租户上下文（Gin 中间件 + context 传递） |
| `event` | 轻量事件总线（同步/异步发布 + panic 恢复） |
| `cache` | 缓存抽象（Redis 实现 + GetOrLoad 模式）与进程内 LRU |
| `graceful` | HTTP Server 优雅关停（信号监听 + 超时控制） |
| `obs` | 结构化可观测性字段、请求元数据、slog wrapper、出站 Request-ID 传播 |
| `utils` | IP、手机号、指数退避等无状态通用工具 |
| `runtimeguard` | 运行时安全守卫（内存/Redis 固定窗口限流、短租约锁、Gin fail-closed middleware） |

## 使用示例

```go
import (
    "github.com/gin-gonic/gin"
    "github.com/zanehu-ai/x-go/config"
    "github.com/zanehu-ai/x-go/db"
    "github.com/zanehu-ai/x-go/logger"
    "github.com/zanehu-ai/x-go/resp"
    "github.com/zanehu-ai/x-go/middleware"
)

func main() {
    cfg := config.Load()
    log := logger.New(cfg.Env)
    gormDB, _ := db.New(cfg.MySQL)

    r := gin.New()
    r.Use(middleware.RequestIDMiddleware())
    r.Use(middleware.CORSMiddleware("http://localhost:3000"))

    r.GET("/health", func(c *gin.Context) {
        resp.Success(c, gin.H{"status": "ok"})
    })
}
```

## API 响应格式

```json
{
  "code": 0,
  "message": "success",
  "data": { ... },
  "trace_id": "abc-123"
}
```

错误码规范：`0` 成功 / `1xxx` 客户端错误 / `2xxx` 鉴权错误 / `5xxx` 服务端错误

## 开发

```bash
make test          # 运行单元测试
make test-verbose  # 详细输出
make lint          # golangci-lint
make coverage      # 生成覆盖率报告
```

## 版本管理

推送 tag 发布新版本（模块路径变更后的第一版是 `v0.6.0`，见上方 Module path change）：
```bash
git tag v0.6.0
git push origin v0.6.0
```

消费方更新：
```bash
go get github.com/zanehu-ai/x-go@v0.6.0
```
