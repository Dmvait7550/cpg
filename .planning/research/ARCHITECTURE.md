# Architecture Research

**Domain:** Go CLI tool -- gRPC streaming client with Kubernetes CRD generation
**Researched:** 2026-03-08
**Confidence:** HIGH

## System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                         CLI Layer (cobra)                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐   │
│  │ observe   │  │ generate │  │  root    │  │ version/status   │   │
│  │ command   │  │ command  │  │ command  │  │ commands         │   │
│  └─────┬─────┘  └────┬─────┘  └─────────┘  └──────────────────┘   │
│        │              │                                             │
├────────┴──────────────┴─────────────────────────────────────────────┤
│                      Core Pipeline                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐   │
│  │ hubble   │→ │ policy   │→ │ dedup    │→ │ output           │   │
│  │ (source) │  │ (build)  │  │ (filter) │  │ (write YAML)     │   │
│  └─────┬────┘  └──────────┘  └─────┬────┘  └──────────────────┘   │
│        │                           │                                │
├────────┴───────────────────────────┴────────────────────────────────┤
│                      Infrastructure                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                         │
│  │ gRPC     │  │ client-go│  │ filesystem│                         │
│  │ (relay)  │  │ (k8s)   │  │ (files)   │                         │
│  └──────────┘  └──────────┘  └──────────┘                         │
└─────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|------------------------|
| `cmd/` (cobra commands) | Parse flags, wire dependencies, run pipeline | Thin wiring layer, no business logic |
| `pkg/hubble` | Connect to Hubble Relay gRPC, stream dropped flows | gRPC client with `observer.ObserverClient.GetFlows()`, manages port-forward lifecycle |
| `pkg/policy` | Transform flow data into CiliumNetworkPolicy structs | Pure function: `[]Flow -> []CiliumNetworkPolicy`, uses `pkg/policy/api` types |
| `pkg/labels` | Smart label selection from flow metadata | Extracts `app.kubernetes.io/*` and workload labels, builds `EndpointSelector` |
| `pkg/dedup` | Deduplicate policies against local files and cluster state | Compares generated policies against existing ones, returns only net-new |
| `pkg/k8s` | Kubernetes client for reading existing CNPs and port-forwarding | `client-go` with Cilium CRD clientset, `tools/portforward` for relay access |
| `pkg/output` | Serialize policies to YAML files in organized directory structure | `sigs.k8s.io/yaml` marshaling, one file per policy, deterministic naming |

## Recommended Project Structure

```
cpg/
├── main.go                    # Entry point, calls cmd.Execute()
├── cmd/
│   ├── root.go                # Root command, global flags (--server, --kubeconfig, --log-level)
│   ├── generate.go            # Main command: observe flows + generate policies
│   └── version.go             # Version/build info
├── pkg/
│   ├── hubble/
│   │   ├── client.go          # gRPC client wrapper, GetFlows streaming
│   │   ├── portforward.go     # Auto port-forward to hubble-relay service
│   │   └── filter.go          # Flow filter construction (verdict=DROPPED, namespace, labels)
│   ├── policy/
│   │   ├── builder.go         # Flow -> CiliumNetworkPolicy conversion
│   │   ├── ingress.go         # Ingress rule construction
│   │   ├── egress.go          # Egress rule construction
│   │   └── cidr.go            # CIDR-based rules for external traffic
│   ├── labels/
│   │   ├── selector.go        # Smart label extraction from flow metadata
│   │   └── priority.go        # Label priority ordering (app.kubernetes.io > workload > pod)
│   ├── dedup/
│   │   ├── local.go           # Deduplicate against files in output directory
│   │   └── cluster.go         # Deduplicate against live CNPs via client-go
│   ├── k8s/
│   │   ├── client.go          # Kubernetes/Cilium clientset factory
│   │   └── cnp.go             # CiliumNetworkPolicy list/get operations
│   └── output/
│       ├── writer.go          # YAML serialization and file writing
│       └── naming.go          # Deterministic file/directory naming conventions
├── go.mod
├── go.sum
└── Makefile
```

### Structure Rationale

- **`cmd/`:** Cobra commands only handle flag parsing and dependency wiring. Zero business logic. This mirrors the Hubble CLI pattern where `hubble/cmd/` contains thin command handlers that delegate to core packages.
- **`pkg/`:** Domain-driven packages with clear single responsibilities. Each package is independently testable. The `pkg/` prefix signals these are library-quality packages (matches Cilium ecosystem convention).
- **No `internal/`:** For a single-binary CLI tool without external consumers, `internal/` adds ceremony without value. If the tool later becomes a library, promote specific packages.
- **Flat within packages:** Avoid deep nesting. Each package has 2-4 files max. If a package grows beyond that, it is doing too much.

## Architectural Patterns

### Pattern 1: Streaming Pipeline with Channels

**What:** Flow data moves through a pipeline of Go channels: source (gRPC stream) -> transform (policy builder) -> filter (dedup) -> sink (file writer). Each stage runs in its own goroutine.

**When to use:** Always -- this is the core architecture. gRPC streaming naturally produces events that flow through processing stages.

**Trade-offs:** Channels add complexity vs. a synchronous loop, but they decouple stages, enable backpressure, and allow the dedup stage to batch work.

**Example:**
```go
// Pipeline wiring in cmd/generate.go
func runGenerate(ctx context.Context, cfg Config) error {
    // Stage 1: Source -- gRPC stream to channel
    flows := make(chan *flow.Flow, 256)
    go hubble.StreamDroppedFlows(ctx, conn, cfg.Filters, flows)

    // Stage 2: Transform -- flows to policies
    policies := make(chan *v2.CiliumNetworkPolicy, 64)
    go policy.BuildFromFlows(ctx, flows, policies, cfg.LabelStrategy)

    // Stage 3: Filter -- deduplicate
    unique := make(chan *v2.CiliumNetworkPolicy, 64)
    go dedup.Filter(ctx, policies, unique, localStore, clusterStore)

    // Stage 4: Sink -- write YAML files
    return output.WriteAll(ctx, unique, cfg.OutputDir)
}
```

### Pattern 2: Aggregation Window Before Policy Generation

**What:** Rather than generating a policy per individual flow, accumulate flows over a short window (e.g., 5 seconds or N flows) and merge them into a single policy per (namespace, workload, direction) tuple. This prevents generating 100 identical policies for 100 dropped packets.

**When to use:** Always. Without aggregation, high-traffic workloads produce massive duplicates.

**Trade-offs:** Adds latency (user waits for window to fill). A configurable window or "flush on idle" approach balances responsiveness with dedup quality.

**Example:**
```go
// In pkg/policy/builder.go
type FlowAggregator struct {
    window   time.Duration
    policies map[policyKey]*policyAccumulator // keyed by (ns, workload, direction)
}

type policyKey struct {
    Namespace string
    Workload  string
    Direction string // "ingress" or "egress"
}
```

### Pattern 3: Interface-Based Infrastructure Boundaries

**What:** Define interfaces at package boundaries for infrastructure concerns (gRPC client, Kubernetes client, filesystem). Concrete implementations live in the infrastructure packages; core logic depends only on interfaces.

**When to use:** For all external dependencies (gRPC, Kubernetes, filesystem). Enables testing without real clusters.

**Trade-offs:** More interfaces upfront, but massively simplifies testing. The policy builder can be tested with mock flows without any gRPC connection.

**Example:**
```go
// In pkg/hubble/client.go
type FlowSource interface {
    StreamFlows(ctx context.Context, filters FlowFilters) (<-chan *flow.Flow, error)
}

// In pkg/dedup/cluster.go
type PolicyLister interface {
    ListCNPs(ctx context.Context, namespace string) ([]*v2.CiliumNetworkPolicy, error)
}

// In pkg/output/writer.go
type PolicyWriter interface {
    Write(ctx context.Context, policy *v2.CiliumNetworkPolicy) error
}
```

## Data Flow

### Main Flow: Dropped Flows to YAML Files

```
Hubble Relay (gRPC server, port 4245)
    │
    │ GetFlows(GetFlowsRequest{Follow: true, Whitelist: [verdict=DROPPED]})
    │ Server-streaming RPC over HTTP/2
    ↓
pkg/hubble.Client
    │ Receives GetFlowsResponse, extracts flow.Flow
    │ Filters by namespace/labels if specified
    ↓
pkg/policy.Builder
    │ Groups flows by (namespace, workload, direction)
    │ Aggregates ports across flows for same source->dest pair
    │ Constructs api.Rule with EndpointSelector + IngressRule/EgressRule
    │ Wraps in v2.CiliumNetworkPolicy with TypeMeta/ObjectMeta
    ↓
pkg/dedup.Filter
    │ Checks against local file store (output dir scan)
    │ Checks against cluster store (client-go CNP list)
    │ Passes through only net-new policies
    ↓
pkg/output.Writer
    │ Marshals CiliumNetworkPolicy to YAML via sigs.k8s.io/yaml
    │ Writes to <output-dir>/<namespace>/<workload>-<direction>.yaml
    ↓
Filesystem (organized YAML files)
```

### Connection Setup Flow

```
CLI start
    │
    ├─ --server flag provided?
    │   YES → Direct gRPC dial to address
    │   NO  → Auto port-forward flow:
    │          ├─ Build client-go config (kubeconfig/in-cluster)
    │          ├─ Find hubble-relay pod in kube-system
    │          ├─ Start port-forward (client-go/tools/portforward)
    │          ├─ Wait for Ready channel
    │          └─ gRPC dial to localhost:<forwarded-port>
    │
    └─ Establish observer.ObserverClient
```

### Key Data Flows

1. **Flow ingestion:** Hubble Relay streams `flow.Flow` proto messages. Each flow contains source/destination identity, labels, namespace, pod name, L4 port/protocol, traffic direction, and verdict. The tool filters for `verdict=DROPPED`.

2. **Policy construction:** Flows are grouped by `(target namespace, target workload, direction)`. For ingress: the target is `destination`, peers are `source`. For egress: the target is `source`, peers are `destination`. Ports from multiple flows are merged into a single `PortRule`.

3. **Label extraction:** From flow metadata, `pkg/labels` picks the best labels for `EndpointSelector`: prefers `app.kubernetes.io/name` > `app` > workload name derived from pod name. For peers, same logic applies. If peer is outside the cluster (no labels, only IP), a `CIDRRule` is generated instead.

4. **Dedup comparison:** Generated policies are compared semantically (not string-equal). Two policies match if they have the same endpoint selector, direction, and cover the same or subset of port/peer combinations.

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|--------------------------|
| Single namespace, few workloads | Default config works fine. Synchronous dedup, small aggregation window. |
| Many namespaces, dozens of workloads | Increase channel buffer sizes. Parallel dedup checks per namespace. Consider namespace-scoped goroutines for policy building. |
| Large cluster, thousands of flows/sec | Flow aggregation window becomes critical. May need to increase gRPC receive buffer. Consider rate-limiting output writes to avoid filesystem thrashing. |

### Scaling Priorities

1. **First bottleneck:** Policy aggregation. Without proper grouping, the tool generates thousands of near-identical policies. Aggregation by `(namespace, workload, direction)` is the single most important design decision.
2. **Second bottleneck:** Cluster dedup (listing existing CNPs). For large clusters with many policies, the `ListCNPs` call can be slow. Use a label selector or informer cache if this becomes an issue, but start simple with direct list calls.

## Anti-Patterns

### Anti-Pattern 1: One Policy Per Flow

**What people do:** Generate a separate CiliumNetworkPolicy for each individual dropped flow.
**Why it is wrong:** A workload with 50 denied connections to the same peer on different ports generates 50 policies instead of 1. Unusable output that is impossible to review or apply.
**Do this instead:** Aggregate flows by `(namespace, workload, direction)` and merge ports/peers into a single policy per workload per direction.

### Anti-Pattern 2: String-Based YAML Templating

**What people do:** Build YAML by concatenating strings or using `text/template` with raw YAML.
**Why it is wrong:** Produces invalid YAML on edge cases (special characters in labels, multiline values). Cannot validate structure at compile time. Loses type safety from Cilium's Go types.
**Do this instead:** Build `v2.CiliumNetworkPolicy` structs using Cilium's Go types, then marshal with `sigs.k8s.io/yaml`. The compiler catches structural errors, and marshaling handles escaping.

### Anti-Pattern 3: Blocking gRPC Stream Processing

**What people do:** Process each flow synchronously in the gRPC receive loop (build policy, dedup, write file, then receive next flow).
**Why it is wrong:** File I/O and Kubernetes API calls block the gRPC stream. If processing is slower than flow arrival, the gRPC buffer fills and flows are dropped or the connection stalls.
**Do this instead:** Use the channel pipeline pattern. The gRPC receive goroutine only reads and sends to a channel. Processing happens in separate goroutines with buffered channels providing backpressure.

### Anti-Pattern 4: Global Kubernetes Client Construction

**What people do:** Create `kubernetes.Clientset` at package init or as a global variable.
**Why it is wrong:** Breaks testability, makes it impossible to run without a cluster, couples packages to infrastructure.
**Do this instead:** Accept interfaces at package boundaries. Construct clients in `cmd/` and inject them. Tests provide mocks.

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| Hubble Relay | gRPC streaming client via `observer.NewObserverClient()` | Server-streaming RPC. Connection requires either direct address or port-forward. Use `grpc.WithBlock()` for initial connection, then stream with `Follow: true`. |
| Kubernetes API | `client-go` with Cilium CRD clientset | Used for two things: (1) port-forwarding to hubble-relay pod, (2) listing existing CiliumNetworkPolicies for dedup. Use `pkg/k8s/client/clientset/versioned` for typed CNP access. |
| Filesystem | Standard `os` package | Write YAML files. Create directories as needed. Scan existing files for local dedup. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `cmd/` -> `pkg/*` | Direct function calls, dependency injection | Commands construct dependencies and call into pkg functions. No pkg imports cmd. |
| `pkg/hubble` -> `pkg/policy` | Channel of `*flow.Flow` | Hubble package produces flows, policy package consumes them. No direct import between them -- connected via channels in cmd. |
| `pkg/policy` -> `pkg/labels` | Direct function calls | Policy builder calls label selector to build `EndpointSelector` from flow metadata. Tight coupling is fine -- labels is a helper for policy. |
| `pkg/policy` -> `pkg/dedup` | Channel of `*v2.CiliumNetworkPolicy` | Policy produces, dedup filters. Connected via channels in cmd. |
| `pkg/dedup` -> `pkg/k8s` | Interface (`PolicyLister`) | Dedup calls k8s to list existing CNPs. Uses interface for testability. |
| `pkg/dedup` -> `pkg/output` | Reads existing files | Local dedup scans output directory. Could share a `PolicyStore` interface with output writer. |

## Suggested Build Order

Based on component dependencies, build in this order:

1. **pkg/labels + pkg/policy (core logic, no infrastructure)**
   - Pure transformation functions, fully testable with mock data
   - No gRPC, no Kubernetes, no filesystem needed
   - This is the hardest domain logic -- get it right first

2. **pkg/output (filesystem only)**
   - YAML marshaling and file writing
   - Depends on policy types but nothing else
   - Easy to test with temp directories

3. **pkg/hubble (gRPC client)**
   - Requires understanding of Hubble proto API
   - Test with a mock gRPC server or against a real cluster
   - Port-forward logic can be deferred (use `--server` flag first)

4. **cmd/ (wiring)**
   - Cobra commands that wire the pipeline together
   - Depends on all pkg packages being functional
   - Thin layer, mostly configuration

5. **pkg/dedup (local files)**
   - Compares generated policies against output directory
   - Depends on output package for file format consistency

6. **pkg/k8s + pkg/dedup cluster (Kubernetes integration)**
   - Client-go setup, CNP listing, port-forward
   - Most complex infrastructure, defer until pipeline works end-to-end
   - Test against a real cluster with Cilium installed

## Key Cilium/Hubble Types Reference

These are the primary types from `github.com/cilium/cilium` that CPG will use:

**Flow observation (input):**
- `github.com/cilium/cilium/api/v1/observer.ObserverClient` -- gRPC client interface
- `github.com/cilium/cilium/api/v1/observer.GetFlowsRequest` -- stream request with filters
- `github.com/cilium/cilium/api/v1/flow.Flow` -- individual network flow with metadata

**Policy generation (output):**
- `github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2.CiliumNetworkPolicy` -- CRD type
- `github.com/cilium/cilium/pkg/policy/api.Rule` -- policy rule with endpoint selector, ingress/egress rules
- `github.com/cilium/cilium/pkg/policy/api.IngressRule` -- FromEndpoints, FromCIDR, ToPorts
- `github.com/cilium/cilium/pkg/policy/api.EgressRule` -- ToEndpoints, ToCIDR, ToPorts
- `github.com/cilium/cilium/pkg/policy/api.EndpointSelector` -- wraps LabelSelector
- `github.com/cilium/cilium/pkg/policy/api.PortRule` -- port + protocol specification
- `github.com/cilium/cilium/pkg/policy/api.CIDRRule` -- CIDR block for external traffic

**Kubernetes integration:**
- `github.com/cilium/cilium/pkg/k8s/client/clientset/versioned` -- typed Cilium CRD clientset
- `k8s.io/client-go/tools/portforward` -- programmatic port-forwarding

## Sources

- [Hubble CLI repository](https://github.com/cilium/hubble)
- [Hubble CLI architecture (DeepWiki)](https://deepwiki.com/cilium/hubble/3-cli-usage-guide)
- [Cilium observer gRPC proto](https://github.com/cilium/cilium/blob/main/api/v1/observer/observer_grpc.pb.go)
- [CiliumNetworkPolicy types (pkg.go.dev)](https://pkg.go.dev/github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2)
- [Cilium policy/api types (pkg.go.dev)](https://pkg.go.dev/github.com/cilium/cilium/pkg/policy/api)
- [Cilium CNP client-go](https://github.com/cilium/cilium/blob/master/pkg/k8s/client/clientset/versioned/typed/cilium.io/v2/ciliumnetworkpolicy.go)
- [client-go port-forward package](https://pkg.go.dev/k8s.io/client-go/tools/portforward)
- [Hubble internals (Cilium docs)](https://docs.cilium.io/en/stable/internals/hubble/)
- [Go CLI structure best practices](https://www.bytesizego.com/blog/structure-go-cli-app)
- [Kubernetes CLI development patterns](https://iximiuz.com/en/posts/kubernetes-api-go-cli/)

---
*Architecture research for: Cilium Policy Generator CLI*
*Researched: 2026-03-08*
