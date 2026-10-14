# Changelog

## [0.6.0] - 2026-10-14

Prepared release candidate. The version tag and module publication remain
pending the required GitHub review and merge; this entry is not publication
evidence.

### Changed

- Adopt `github.com/zanehu-ai/x-go` as the Go module and import identity.
- Align examples and source comments with the new identity while preserving
  implementation, exported package paths and cross-language fixture behavior.
- Keep historical `synapse-go` versions unchanged. Consumers must explicitly
  adopt the new module after publication; a source release does not deploy them.

### Validation

- Short-mode race tests passed with 72.6% overall coverage; the existing CI
  minimum remains 50%.
- The candidate excludes the separate CORS changes in PRs #9 and #10.
