# Project Research Summary

**Project:** Cilium Policy Generator (CPG)
**Domain:** Go CLI tool -- Kubernetes/Cilium network policy generation from Hubble gRPC
**Researched:** 2026-03-08
**Confidence:** HIGH

## Executive Summary

CPG is a Go CLI tool that connects to Hubble Relay via gRPC, observes dropped network flows in real time, and generates CiliumNetworkPolicy YAML files. This is a niche but well-defined domain: the Kubernetes/Cilium ecosystem provides mature Go libraries for every integration point (gRPC proto types, CRD Go types, client-go for cluster access). The recommended approach is a streaming pipeline architecture where flows move through channels: source (gRPC) -> transform (policy builder) -> filter (dedup) -> sink (YAML writer). No direct open-source competitor does live gRPC streaming to Hubble Relay -- the closest alternative (siegmund-heiss-ich/cnp-generator) works offline on exported JSON files.

The stack is straightforward and high-confidence: Go 1.26, Cobra, grpc-go, and the `cilium/cilium` monorepo for both proto types and CRD types. The main technical risk is the Cilium monorepo dependency pulling hundreds of transitive deps and inflating binary size. This is a known, unavoidable cost when using Cilium's Go types. The alternative (generating proto stubs independently or templating YAML) trades type safety for build simplicity -- not recommended for a tool whose correctness is security-critical.

The most dangerous pitfalls are semantic, not technical: inverting traffic direction when mapping flows to ingress/egress rules, generating CIDR rules for Cilium-managed endpoints (silently ignored), and missing namespace labels in cross-namespace selectors. These bugs produce YAML that applies cleanly but does nothing or creates security holes. Rigorous unit tests with bidirectional flow pairs and multi-namespace scenarios are non-negotiable from the first phase.

## Key Findings

### Recommended Stack

The stack leverages the Kubernetes/Cilium Go ecosystem exclusively. All libraries are mature, actively maintained, and used by the Cilium project itself. No exotic or risky dependencies.

**Core technologies:**
- **Go 1.26.x**: Latest stable. All dependencies compatible. No reason to target older versions.
- **Cobra v1.10.2**: CLI framework. De facto standard in K8s ecosystem (kubectl, cilium, hubble all use it).
- **grpc-go v1.79.x**: Required for Hubble Relay Observer API streaming.
- **cilium/cilium v1.19.1**: Single source for both Hubble proto types (`api/v1/observer`, `api/v1/flow`) and CRD types (`pkg/k8s/apis/cilium.io/v2`, `pkg/policy/api`). Avoids maintaining separate proto definitions.
- **client-go v0.35.x**: Kubernetes API access for port-forwarding and cluster-aware dedup.
- **sigs.k8s.io/yaml v1.6.0**: YAML marshaling that correctly handles K8s `json:` struct tags.
- **zap v1.27.1**: Structured logging per PROJECT.md constraint.

**Critical version note:** `cilium/cilium` pins its own `client-go` version. Use `go mod tidy` to resolve; may need `replace` directives if conflicts arise.

### Expected Features

**Must have (table stakes):**
- Hubble Relay gRPC connection with dropped flow filtering
- Ingress + egress CiliumNetworkPolicy generation
- L3/L4 rules (label selectors + port/protocol)
- Namespace-scoped policies with correct cross-namespace labels
- Smart label selection (prefer `app.kubernetes.io/*`, skip ephemeral labels)
- Valid YAML output with proper TypeMeta/ObjectMeta
- Structured logging

**Should have (differentiators):**
- Auto port-forward to Hubble Relay (UX parity with `hubble` CLI)
- CIDR-based rules for external traffic (`reserved:world` identity)
- Organized directory output (one file per policy, GitOps-friendly)
- File-based and cluster-aware deduplication
- Real-time streaming mode with flow aggregation

**Defer (v2+):**
- L7 policy generation (HTTP, DNS) -- requires Envoy, different architecture
- Policy merging/consolidation
- Cilium audit mode integration
- CiliumClusterwideNetworkPolicy -- different CRD, different semantics
- Auto-apply (`kubectl apply`) -- violates GitOps principles, dangerous

### Architecture Approach

A channel-based streaming pipeline with interface-driven infrastructure boundaries. Each stage (source, transform, filter, sink) runs in its own goroutine connected by buffered channels. Core domain logic (policy building, label selection) is pure and independently testable. Infrastructure (gRPC, Kubernetes, filesystem) is injected via interfaces. Flow aggregation windows prevent the "one policy per flow" anti-pattern.

**Major components:**
1. **pkg/hubble** -- gRPC client, flow streaming, port-forward lifecycle
2. **pkg/policy** -- Flow-to-CiliumNetworkPolicy transformation with aggregation
3. **pkg/labels** -- Smart label extraction and EndpointSelector construction
4. **pkg/dedup** -- Local file and cluster-aware deduplication
5. **pkg/k8s** -- Kubernetes/Cilium clientset, CNP listing
6. **pkg/output** -- YAML serialization, file writing, directory organization
7. **cmd/** -- Thin Cobra command layer, dependency wiring only

### Critical Pitfalls

1. **Traffic direction inversion** -- Hubble's `traffic_direction` is from the observing endpoint's perspective, not the flow direction. Anchor rules to `endpointSelector`: EGRESS means selected endpoint is source, INGRESS means selected endpoint is destination. Unit test with bidirectional flow pairs.

2. **CIDR rules on managed endpoints** -- Cilium ignores CIDR rules when both sides are managed. Only generate `toCIDR`/`fromCIDR` for `reserved:world` identity (numeric 2). Never use pod IPs in CIDR rules.

3. **Cross-namespace selector missing namespace label** -- `fromEndpoints`/`toEndpoints` require explicit `k8s:io.kubernetes.pod.namespace` for cross-namespace peers. Without it, the selector silently matches wrong-namespace pods.

4. **Hubble ring buffer overflow** -- Default 4096-event buffer overflows in high-traffic clusters. `LostEvent` messages MUST be handled and surfaced to users. Never claim policy completeness if events were lost.

5. **Cilium monorepo dependency weight** -- Importing `cilium/cilium` pulls hundreds of transitive deps, inflates binary to ~40+ MiB. Pin exact version, gate CI on binary size, accept the trade-off for type safety.

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: Project Scaffolding and Core Policy Logic

**Rationale:** Architecture research shows policy building and label selection are pure domain logic with zero infrastructure dependencies. Build and thoroughly test these first -- they contain the hardest correctness requirements (direction mapping, namespace scoping, label heuristics). Getting this wrong propagates bugs to every later phase.
**Delivers:** Go module, project structure, pkg/policy, pkg/labels, pkg/output with unit tests
**Addresses:** Smart label selection, ingress/egress rule construction, port/protocol extraction, valid YAML output
**Avoids:** Traffic direction inversion (Pitfall 1), namespace scoping errors (Pitfall 3), CIDR on managed endpoints (Pitfall 2)

### Phase 2: Hubble gRPC Connection and Flow Streaming

**Rationale:** The gRPC client is the foundation input -- no flows means no policies. Depends on Phase 1 types being defined but not on policy logic being wired. Port-forward and TLS configuration must be validated against real clusters early.
**Delivers:** pkg/hubble with gRPC streaming, flow filtering, LostEvent handling, `--server` flag for direct connection
**Addresses:** Hubble flow ingestion, namespace/label filtering, dropped verdict filtering
**Avoids:** Ring buffer overflow silence (Pitfall 4), TLS/ALPN handshake failures (Pitfall 8)

### Phase 3: End-to-End Pipeline and CLI Wiring

**Rationale:** With policy logic and flow source both working, wire the channel-based pipeline in cmd/generate. This is the integration point where the streaming architecture pattern is realized. Aggregation window logic lives here.
**Delivers:** Working `cpg generate` command that connects to Hubble, generates policies, writes YAML
**Addresses:** Real-time streaming generation, flow aggregation, organized file output
**Avoids:** Blocking gRPC stream processing (Architecture anti-pattern 3), one-policy-per-flow (Architecture anti-pattern 1)

### Phase 4: Auto Port-Forward and Cluster Deduplication

**Rationale:** Both features require client-go and share the Kubernetes client. Cluster dedup is a key differentiator (no OSS competitor has it). Auto port-forward removes the biggest UX friction point.
**Delivers:** pkg/k8s, auto port-forward, cluster-aware dedup, file-based dedup
**Addresses:** Auto port-forward, cluster-aware deduplication, file deduplication
**Avoids:** Global client construction anti-pattern, stale port-forward connections (Pitfall 8)

### Phase 5: CIDR Policies and Hardening

**Rationale:** CIDR rules are high-risk (Pitfall 2) and should be added after core ingress/egress is proven correct. This phase also adds production hardening: reconnection logic, graceful shutdown, summary output.
**Delivers:** CIDR-based egress/ingress rules for external traffic, reconnection, UX polish
**Addresses:** CIDR policies for external traffic, streaming reconnection, user-facing summaries
**Avoids:** CIDR on managed endpoints (Pitfall 2), toServices+toPorts combination (Pitfall 7), insufficient flow data warnings (Pitfall 6)

### Phase Ordering Rationale

- **Core logic first (Phase 1):** Architecture research explicitly recommends building pure domain logic before infrastructure. Policy correctness is the security foundation.
- **gRPC second (Phase 2):** Depends on flow types from Phase 1. Must validate real cluster connectivity before integration.
- **Integration third (Phase 3):** Wires Phase 1 + Phase 2. Cannot be done earlier. Delivers first usable tool.
- **K8s features fourth (Phase 4):** Both port-forward and cluster dedup share client-go. Deferred because `--server` flag provides connectivity in Phase 2-3.
- **CIDR last (Phase 5):** Highest pitfall density. Requires all prior phases to be correct. Adds external traffic support as a layer on proven internals.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 2 (Hubble gRPC):** TLS/insecure configuration nuances, flow filter API specifics, LostEvent handling -- worth a `/gsd:research-phase` to pin down exact gRPC setup code and reconnection patterns.
- **Phase 4 (client-go integration):** Port-forward programmatic API is under-documented. Cilium CRD clientset registration has gotchas. Research recommended.

Phases with standard patterns (skip research-phase):
- **Phase 1 (Core logic):** Well-defined domain. Cilium Go types are documented on pkg.go.dev. Policy structure is clear from CRD spec.
- **Phase 3 (CLI wiring):** Standard Cobra + channel pipeline. Architecture research provides the exact pattern.
- **Phase 5 (CIDR):** Extension of Phase 1 logic. Pitfalls are well-documented. Pattern is clear: check identity, generate CIDR only for world.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All libraries are K8s ecosystem standards. Versions verified against release pages. No speculative choices. |
| Features | MEDIUM | Niche domain with few direct competitors. Feature landscape derived from competitor analysis + Cilium ecosystem conventions. Core features are clear; differentiator value is estimated. |
| Architecture | HIGH | Channel-based pipeline is standard Go concurrency. Pattern mirrors Hubble CLI internals. Component boundaries are clean and well-justified. |
| Pitfalls | HIGH | Verified against Cilium docs, Datadog post-mortem, Hubble issue tracker. Direction inversion and CIDR-on-managed-endpoints are well-documented failure modes. |

**Overall confidence:** HIGH

### Gaps to Address

- **Cilium monorepo dependency impact:** Actual binary size and build time unknown until `go mod tidy` is run. May need to evaluate proto-only generation if binary exceeds 50 MiB. Validate in Phase 1 scaffolding.
- **Hubble Relay reconnection patterns:** No official documentation on gRPC stream recovery after relay pod restart. Must be tested empirically in Phase 2.
- **Flow label completeness:** Unclear how consistently Hubble populates `app.kubernetes.io/*` labels vs raw pod labels. Label selection heuristics may need tuning based on real cluster data.
- **Cilium CRD clientset usage:** The `pkg/k8s/client/clientset/versioned` package is lightly documented. Phase 4 should validate typed CNP listing works as expected.

## Sources

### Primary (HIGH confidence)
- [Cilium official documentation](https://docs.cilium.io/en/stable/) -- policy language, Hubble internals, audit mode
- [Cilium/cilium GitHub releases](https://github.com/cilium/cilium/releases) -- v1.19.1 types and API
- [pkg.go.dev Cilium packages](https://pkg.go.dev/github.com/cilium/cilium) -- observer, flow, CRD types, policy/api
- [Datadog: CiliumNetworkPolicy Misconfigurations](https://www.datadoghq.com/blog/cilium-network-policy-misconfigurations/) -- toServices+toPorts pitfall
- [client-go portforward package](https://pkg.go.dev/k8s.io/client-go/tools/portforward) -- programmatic port-forward
- [Go Release History](https://go.dev/doc/devel/release) -- Go 1.26.1

### Secondary (MEDIUM confidence)
- [siegmund-heiss-ich/cilium-network-policy-generator](https://github.com/siegmund-heiss-ich/cilium-network-policy-generator) -- competitor analysis, feature baseline
- [Inspektor Gadget Network Policy Advisor](https://inspektor-gadget.io/) -- alternative approach comparison
- [Scott Lowe: Endpoint Selectors and Namespaces](https://blog.scottlowe.org/2024/05/30/endpoint-selectors-and-kubernetes-namespaces-in-ciliumnetworkpolicies/) -- namespace scoping pitfall
- [Hubble CLI repository](https://github.com/cilium/hubble) -- CLI architecture patterns

### Tertiary (LOW confidence)
- [ARMO Platform](https://www.armosec.io/) -- commercial competitor, limited public technical detail
- [Calico Enterprise Policy Recommendation](https://www.tigera.io/) -- commercial competitor, different dataplane

---
*Research completed: 2026-03-08*
*Ready for roadmap: yes*
