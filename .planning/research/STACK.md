# Stack Research

**Domain:** Go CLI tool — Kubernetes/Cilium network policy generation from Hubble gRPC
**Researched:** 2026-03-08
**Confidence:** HIGH

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended | Confidence |
|------------|---------|---------|-----------------|------------|
| Go | 1.26.x | Language runtime | Latest stable (1.26.1 released 2026-03-05). PROJECT.md says 1.23+ but no reason not to target latest. Iterators/rangefunc stable since 1.23, improved toolchain in 1.26. | HIGH |
| `github.com/spf13/cobra` | v1.10.2 | CLI framework | De facto standard for Go CLIs. Used by kubectl, cilium-cli, hubble. Consistent UX for users familiar with K8s tooling. | HIGH |
| `google.golang.org/grpc` | v1.79.x | gRPC client | Required for Hubble Relay Observer API. Latest stable, actively maintained. | HIGH |
| `github.com/cilium/cilium` | v1.19.1 | Hubble proto types + CNP CRD types | Single dependency for both gRPC observer client stubs AND CiliumNetworkPolicy CRD Go types. Avoids maintaining separate proto definitions. | HIGH |
| `k8s.io/client-go` | v0.35.x | Kubernetes API client | Required for cluster dedup (list existing CNPs) and programmatic port-forward to hubble-relay service. Version tracks Kubernetes 1.35.x. | HIGH |
| `go.uber.org/zap` | v1.27.1 | Structured logging | PROJECT.md constraint. Performant, zero-allocation, widely used in K8s ecosystem. | HIGH |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `sigs.k8s.io/yaml` | v1.6.0 | YAML marshaling for K8s objects | Serializing CiliumNetworkPolicy to YAML files. Handles JSON tags to YAML conversion correctly (unlike raw go-yaml). | HIGH |
| `k8s.io/client-go/tools/portforward` | (bundled with client-go) | Programmatic port-forward | Auto port-forward to hubble-relay ClusterIP service, matching hubble CLI UX. Uses SPDY upgrade under the hood. | HIGH |
| `k8s.io/apimachinery` | v0.35.x | K8s meta types (ObjectMeta, TypeMeta, labels) | Required for constructing CiliumNetworkPolicy objects with proper K8s metadata. Pulled transitively by client-go but import directly for types. | HIGH |
| `k8s.io/client-go/tools/clientcmd` | (bundled with client-go) | Kubeconfig loading | Loading kubeconfig for both port-forward and cluster CNP listing. Respect KUBECONFIG env and `--kubeconfig` flag. | HIGH |
| `github.com/stretchr/testify` | v1.9.x | Test assertions | `assert` and `require` packages for readable test assertions. Standard in Go ecosystem. | HIGH |

### Development Tools

| Tool | Version | Purpose | Notes |
|------|---------|---------|-------|
| golangci-lint | v2.11.1 | Linting | Meta-linter. Configure `.golangci.yml` with errcheck, govet, staticcheck, gosec, unused, ineffassign at minimum. | HIGH |
| goreleaser | latest | Binary distribution | Cross-compile and release Go binaries. Configure for linux/darwin amd64/arm64. Use when ready to distribute. | MEDIUM |
| buf | latest | Proto validation (optional) | Only if you need to regenerate proto stubs. Since we consume cilium/cilium module directly, NOT needed. | LOW |

## Installation

```bash
# Initialize Go module
go mod init github.com/<org>/cpg

# Core dependencies
go get github.com/spf13/cobra@v1.10.2
go get go.uber.org/zap@v1.27.1
go get google.golang.org/grpc@latest
go get github.com/cilium/cilium@v1.19.1
go get k8s.io/client-go@v0.35.2
go get k8s.io/apimachinery@v0.35.2
go get sigs.k8s.io/yaml@v1.6.0

# Test dependencies
go get github.com/stretchr/testify@latest

# Dev tools (install globally)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.1
```

## Key Import Paths

These are the critical import paths within `github.com/cilium/cilium` that CPG will use:

```go
// Hubble gRPC client (observer service)
import observer "github.com/cilium/cilium/api/v1/observer"

// Hubble flow types (source/dest, verdict, ports)
import flow "github.com/cilium/cilium/api/v1/flow"

// CiliumNetworkPolicy CRD Go types
import ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"

// Cilium API policy types (rules, specs, endpoint selectors)
import api "github.com/cilium/cilium/pkg/policy/api"
```

The `observer.ObserverClient` interface provides `GetFlows()` which returns a stream of `observer.GetFlowsResponse` containing `flow.Flow` objects with verdict, source/dest identity, labels, ports, and traffic direction.

The `ciliumv2.CiliumNetworkPolicy` type embeds `metav1.ObjectMeta` and contains `*api.Rule` for ingress/egress specs with `api.EndpointSelector`, `api.IngressRule`, `api.EgressRule`, and `api.PortRule`.

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `cobra` | `urfave/cli/v3` | Never for this project. cobra is the K8s ecosystem standard. Switching would confuse users familiar with kubectl/cilium/hubble CLI patterns. |
| `zap` | `log/slog` (stdlib) | If starting fresh without ecosystem constraint. slog is lighter but lacks zap's performance and ecosystem integration. PROJECT.md explicitly chose zap. |
| `cilium/cilium` module | Vendored `.proto` files + protoc | If cilium/cilium module causes dependency hell (unlikely with Go modules). Vendoring protos adds maintenance burden for zero benefit. |
| `sigs.k8s.io/yaml` | `gopkg.in/yaml.v3` | Never for K8s CRD output. `sigs.k8s.io/yaml` correctly handles JSON struct tags which K8s types use exclusively. `gopkg.in/yaml.v3` would require duplicate yaml struct tags. |
| `client-go` portforward | Shell out to `kubectl port-forward` | Never. Shelling out is fragile, requires kubectl on PATH, and loses error handling. `client-go/tools/portforward` is what cilium-cli uses internally. |
| `testify` | stdlib `testing` only | For simple unit tests. Use testify for anything with complex assertions — the readability benefit is significant. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `gopkg.in/yaml.v3` directly | K8s types use `json:` struct tags, not `yaml:` tags. Raw go-yaml produces incorrect field names. | `sigs.k8s.io/yaml` (JSON-to-YAML bridge) |
| `encoding/json` for YAML output | Produces JSON, not YAML. Users expect YAML for K8s manifests. | `sigs.k8s.io/yaml` |
| `viper` for config | Overkill. CPG is a CLI tool with flags, not a config-heavy application. Cobra handles flags natively. | Cobra flags + env vars via `cobra.Command.Flags()` |
| `github.com/cilium/hubble` (standalone hubble repo) | This is the hubble CLI binary, not a library. It imports cilium/cilium internally. Importing it pulls the entire CLI as a dependency. | `github.com/cilium/cilium` (the monorepo with proto types and CRD types) |
| `k8s.io/kubectl` | Massive dependency. Only needed if you want kubectl's exact port-forward behavior. `client-go/tools/portforward` is the extracted library. | `k8s.io/client-go/tools/portforward` |
| `protoc` / `buf generate` | No need to regenerate proto stubs. `cilium/cilium` module ships pre-generated Go code. Generating from .proto adds a build step for zero value. | Import generated code from `cilium/cilium` directly |
| `logrus` | Deprecated in favor of slog/zap in the Go ecosystem. Slower than zap. Some older K8s libraries still use it but it should not be chosen for new code. | `go.uber.org/zap` |

## Stack Patterns

**For gRPC connection to Hubble Relay:**
- Use `grpc.NewClient()` (not deprecated `grpc.Dial()`) with `grpc.WithTransportCredentials(insecure.NewCredentials())` for local port-forwarded connections
- Create `observer.NewObserverClient(conn)` and call `GetFlows()` with `&observer.GetFlowsRequest{Whitelist: [...]}` to filter dropped flows
- The flow filter should use `flow.Verdict_DROPPED` to match only denied traffic

**For port-forward lifecycle:**
- Use `k8s.io/client-go/tools/portforward.New()` with a stop channel
- Run port-forward in a goroutine, wait on the Ready channel before connecting gRPC
- Clean up on context cancellation (SIGINT/SIGTERM)

**For YAML output:**
- Set `TypeMeta` (apiVersion: `cilium.io/v2`, kind: `CiliumNetworkPolicy`) explicitly — K8s types do not auto-populate these
- Use `sigs.k8s.io/yaml.Marshal()` which handles `json:` tags correctly
- Write `---\n` separator if multiple docs per file (but PROJECT.md says one file per policy)

## Version Compatibility

| Package | Compatible With | Notes |
|---------|-----------------|-------|
| `cilium/cilium` v1.19.x | `k8s.io/client-go` v0.31-v0.35 | Cilium pins its own client-go version. Use `go mod tidy` to resolve — Go modules handle this well. May need `replace` directives if version conflicts arise. |
| `k8s.io/client-go` v0.35.x | Go 1.24+ | K8s 1.35 client-go requires Go 1.24 minimum. Compatible with Go 1.26. |
| `grpc-go` v1.79.x | Go 1.23+ | No compatibility issues expected. |
| `cobra` v1.10.x | Go 1.16+ | Broadly compatible, no issues. |
| `zap` v1.27.x | Go 1.19+ | Broadly compatible, no issues. |

**Dependency conflict warning:** The `cilium/cilium` monorepo has a large dependency tree. Expect `go mod tidy` to pull ~200+ transitive dependencies. This is normal and unavoidable when using Cilium's Go types. Build times for the first compile will be longer than typical CLI tools. Subsequent builds use the module cache.

## Sources

- [Go Release History](https://go.dev/doc/devel/release) — Go 1.26.1 released 2026-03-05 (HIGH confidence)
- [spf13/cobra on pkg.go.dev](https://pkg.go.dev/github.com/spf13/cobra) — v1.10.2 published 2025-12-03 (HIGH confidence)
- [go.uber.org/zap on pkg.go.dev](https://pkg.go.dev/go.uber.org/zap) — v1.27.1 published 2025-11-19 (HIGH confidence)
- [cilium/cilium releases](https://github.com/cilium/cilium/releases) — v1.19.1 latest stable (HIGH confidence)
- [cilium/cilium observer proto](https://pkg.go.dev/github.com/cilium/cilium/api/v1/observer) — Observer gRPC service definition (HIGH confidence)
- [cilium/cilium flow proto](https://pkg.go.dev/github.com/cilium/cilium/api/v1/flow) — Flow types with verdict, labels, ports (HIGH confidence)
- [cilium/cilium CiliumNetworkPolicy types](https://pkg.go.dev/github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2) — CRD Go types (HIGH confidence)
- [client-go portforward package](https://pkg.go.dev/k8s.io/client-go/tools/portforward) — Programmatic K8s port-forward (HIGH confidence)
- [sigs.k8s.io/yaml on pkg.go.dev](https://pkg.go.dev/sigs.k8s.io/yaml) — v1.6.0 YAML marshaling (HIGH confidence)
- [grpc-go releases](https://github.com/grpc/grpc-go/releases) — v1.79.x latest (HIGH confidence)
- [golangci-lint releases](https://github.com/golangci/golangci-lint/releases) — v2.11.1 released 2026-03-06 (HIGH confidence)
- [Kubernetes releases](https://kubernetes.io/releases/) — K8s 1.35.x / client-go v0.35.x (HIGH confidence)

---
*Stack research for: Cilium Policy Generator (CPG) — Go CLI tool*
*Researched: 2026-03-08*
