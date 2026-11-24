# x-go

<details>
<summary>简体中文简介（点击展开）</summary>

`x-go` 是面向生产环境的 **Go 后端通用基础设施组件库**（`github.com/zanehu-ai/x-go`，Go 1.25+），覆盖配置校验、MySQL/GORM 与 Redis 连接、多租户与 RBAC 鉴权、分布式锁/限流/熔断、持久化幂等防重、事务发件箱（Outbox）、AES-GCM 版本化加密与 PII 脱敏、结构化审计日志与优雅停机等 11 类核心基础包，适用于支付中台与高可靠微服务底座建设。

</details>

Production-ready Go backend foundation library covering configuration, persistence, authentication, tenancy, cryptography, distributed coordination, idempotency, durable outbox events, media storage, notifications, and observability.

This repository is a reusable library (**`github.com/zanehu-ai/x-go`**); consuming services own their application startup, configuration wiring, and business workflows.

## Design Highlights

- **Financial & Concurrency Guardrails**: Distributed locks (`lock`), rate limiting (`ratelimit`), circuit breaking (`circuitbreaker`), lease guards (`runtimeguard`), and persistent request idempotency (`idempotent`, `idempotency`).
- **Durable Delivery & Eventing**: Transactional outbox (`outbox`), reliable event contracts (`reliableevents`), and signed webhooks (`webhook`).
- **Compliance & Security by Default**: Versioned AES-GCM encryption (`secretbox`), PII redaction (`pii`), RBAC & tenant isolation (`auth`, `rbac`, `tenant`), and structured audit trails (`audit`).
- **Unified Operability**: Structured logging (`logger`), dependency readiness (`healthcheck`), telemetry (`obs`), and graceful shutdown (`graceful`).

## Requirements and installation

[go.mod](go.mod) declares Go **1.25.0**; [CI](.github/workflows/ci.yml) uses Go 1.25. Private consumers need Git access to the module and an appropriate `GOPRIVATE` setting. Add the organization pattern to your existing private-module configuration rather than replacing unrelated entries.

From the consuming Go module, select a reviewed tag or commit:

```bash
# Replace <version-or-commit> with the dependency revision you have reviewed.
# After a canonical module release is published:
go get github.com/zanehu-ai/x-go@<version-or-commit>
```

Keep the chosen version in the consumer's `go.mod` and `go.sum`. Existing consumers of older module identities require their own migration; changing a checkout folder does not migrate imports.

## Package guide

| Area | Packages | Responsibility |
| --- | --- | --- |
| Configuration and connections | [config](config/), [db](db/), [redis](redis/) | Configuration types, environment helpers, validation, MySQL/GORM and Redis clients |
| HTTP and responses | [ginutil](ginutil/), [middleware](middleware/), [resp](resp/), [bizerr](bizerr/) | Gin helpers, request IDs, CORS, auth adapters and response/error envelopes |
| Identity and access | [auth](auth/), [rbac](rbac/), [tenant](tenant/) | Token contracts, permission checks and tenant context |
| Secrets and personal data | [crypto](crypto/), [secretbox](secretbox/README.md), [pii](pii/) | Hashing/TOTP, versioned AES-GCM encryption and redaction |
| Rate limits and coordination | [ratelimit](ratelimit/), [runtimeguard](runtimeguard/), [lock](lock/), [circuitbreaker](circuitbreaker/) | Request limiting, leases, distributed locks and circuit breakers |
| Idempotency | [idempotent](idempotent/), [idempotency](idempotency/) | Middleware/service checks and persistent request/response records |
| Jobs and events | [job](job/), [jobpayload](jobpayload/), [outbox](outbox/), [reliableevents](reliableevents/), [event](event/), [scheduler](scheduler/) | Job contracts, payloads, durable delivery and in-process scheduling/events |
| Files and media | [files](files/), [storage](storage/), [imaging](imaging/) | File metadata, storage drivers, image validation and processing |
| Notifications | [notification](notification/), [notify](notify/), [mailer](mailer/), [webhook](webhook/) | Notification contracts, delivery abstractions, email and signed webhooks |
| Operations | [audit](audit/), [logger](logger/), [obs](obs/), [healthcheck](healthcheck/), [graceful](graceful/) | Audit records, logging, observability, dependency checks and shutdown |
| Data utilities | [cache](cache/), [migrate](migrate/), [sequence](sequence/), [timeutil](timeutil/), [validate](validate/), [utils](utils/) | Caching, SQL migrations, sequences, time, validation and shared helpers |

Package source and adjacent tests define each API. Similar package names represent separate contracts; check the consumer before substituting one for another.

## Minimal HTTP example

This example uses existing request-ID and response helpers and listens on loopback. It does not require MySQL or Redis.

```go
package main

import (
    "log"

    "github.com/gin-gonic/gin"
    "github.com/zanehu-ai/x-go/middleware"
    "github.com/zanehu-ai/x-go/resp"
)

func main() {
    router := gin.New()
    router.Use(gin.Recovery(), middleware.RequestIDMiddleware())
    router.GET("/healthz", func(c *gin.Context) {
        resp.Success(c, gin.H{"status": "ok"})
    })
    log.Fatal(router.Run("127.0.0.1:8080"))
}
```

`/healthz` above is a liveness response. Applications that need readiness checks must register their dependencies using [healthcheck](healthcheck/) or their own readiness contract.

The [response envelope](resp/resp.go) contains `code`, `message`, `data` and an optional `trace_id`. Success is `0`; error constants distinguish request errors (`1xxx`), authorization errors (`2xxx`) and internal errors (`5xxx`). `config` provides types and environment helpers; the application supplies its own configuration loader.

## Development

Run from this repository with Go and, for linting, `golangci-lint` installed:

```bash
go mod download
make test          # Race/coverage checks with -short; skips external-service tests
make test-verbose  # Same scope with verbose output
make lint          # golangci-lint run ./...
go build ./...
make coverage      # Produces coverage.out and coverage.html
```

`make test-all` includes integration tests. Review their fixtures first and use isolated test services: the [DB tests](db/db_test.go) expect a local MySQL test database and the [Redis tests](redis/redis_test.go) use local Redis. They are not production probes.

[The Makefile](Makefile) is the command reference. CI also rejects local filesystem `replace` directives in this library's `go.mod` and applies its configured coverage threshold.

## Versioning and consumers

Versions are selected through Git tags or commit-based Go module versions. Review exported API changes, run the declared checks, then coordinate the release and consumer version updates. A local source change does not update downstream services; each consumer must explicitly adopt the chosen revision and pass its own checks.
