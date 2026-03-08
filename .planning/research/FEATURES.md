# Feature Research

**Domain:** Cilium network policy generation CLI tools
**Researched:** 2026-03-08
**Confidence:** MEDIUM — niche domain with few direct competitors; feature landscape derived from competitor analysis plus Cilium ecosystem conventions

## Feature Landscape

### Table Stakes (Users Expect These)

Features users assume exist. Missing these = product feels incomplete or untrustworthy.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Hubble flow ingestion (dropped/denied verdict filtering) | Core input — every tool in this space reads Hubble flows. Without it, the tool has no data source. | MEDIUM | gRPC streaming via Hubble Relay protobuf API. Must handle reconnects and backpressure. |
| Ingress + egress policy generation | Both directions are fundamental to default-deny posture. Generating only one direction is half a tool. | MEDIUM | Separate rule sets per direction. Need to correctly identify source vs destination based on flow perspective. |
| L3/L4 rules (labels + ports) | Minimum useful policy granularity. Every competitor generates at least L3/L4. | MEDIUM | EndpointSelector with matchLabels + port/protocol tuples. |
| CIDR-based rules for external traffic | Clusters talk to external services (DNS, APIs, registries). Without CIDR rules, external traffic stays blocked. | MEDIUM | Detect world identity or non-cluster IPs in flows, generate toCIDR/fromCIDR rules. |
| Namespace-scoped policies | CiliumNetworkPolicy is namespace-scoped. Users expect policies generated per namespace. | LOW | Map flow namespace to policy metadata.namespace. |
| Label-based endpoint selectors | Policies must use label selectors, not pod names or IPs. This is how Kubernetes policies work. | MEDIUM | Smart label extraction: prefer `app.kubernetes.io/*` labels, fall back to workload name. Avoid ephemeral labels. |
| Valid CiliumNetworkPolicy YAML output | Output must be syntactically valid and apply cleanly with `kubectl apply`. | LOW | Use Cilium Go types for serialization — guarantees schema compliance. |
| Namespace/label filtering on input | Users need to scope observation to specific namespaces or workloads. Observing everything is noisy and slow. | LOW | Pass filters to Hubble GetFlows RPC whitelist. |
| Port + protocol specificity | Policies must specify exact port numbers and protocols (TCP/UDP). Wildcard ports are a security risk. | LOW | Direct mapping from flow port/protocol fields. |
| Deduplication of generated policies | Without dedup, the tool generates the same policy repeatedly for repeated flows. Unusable noise. | MEDIUM | Hash-based dedup on policy content. Must dedup both against previously generated files and against live cluster policies. |

### Differentiators (Competitive Advantage)

Features that set CPG apart from alternatives. Not required, but valuable.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Real-time streaming generation | Most competitors (siegmund-heiss-ich, Inspektor Gadget) work on exported/recorded flow data. CPG generates policies as flows arrive — zero wait time, immediate feedback loop. | HIGH | Continuous gRPC stream processing with incremental policy emission. Must handle flow aggregation windows. |
| Direct gRPC to Hubble Relay (no JSON intermediary) | Eliminates the export-then-parse workflow. Native proto types mean zero serialization bugs. Competitor (siegmund-heiss-ich) works on exported JSON. | MEDIUM | Uses cilium/cilium observer proto directly. Cleaner architecture, fewer moving parts. |
| Auto port-forward to Hubble Relay | UX parity with `hubble` CLI. User does not need to manually set up port-forward or know the relay service address. | MEDIUM | Use client-go to discover hubble-relay service and set up port-forward programmatically. Fallback to `--server` flag. |
| Cluster-aware deduplication | Check existing CiliumNetworkPolicies in the cluster before generating, preventing duplicates of already-applied policies. No existing tool does this. | HIGH | Requires client-go with Cilium CRD types. Must handle partial matches (policy exists but with different ports). |
| One file per policy with organized directory structure | Git-friendly output: easy to review diffs, selectively apply specific policies, integrate with GitOps workflows. Competitor outputs everything to a single directory. | LOW | Directory structure like `policies/<namespace>/<workload>-<direction>.yaml`. |
| Structured logging (zap) | Production-grade observability of the tool itself. Important for SRE teams who need to debug policy generation in CI/CD or long-running observation sessions. | LOW | Standard zap setup with log levels. |
| Smart label selection heuristics | Automatically pick the most stable, meaningful labels for selectors instead of using all labels. Reduces policy churn when pods restart. | MEDIUM | Priority: `app.kubernetes.io/name` > `app` > workload name derived from owner reference. Skip `pod-template-hash`, `controller-revision-hash`, etc. |

### Anti-Features (Commonly Requested, Often Problematic)

Features that seem good but create complexity, security risk, or maintenance burden.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Auto-apply policies (`kubectl apply`) | "Just make it work end-to-end" | Automatically applying network policies without human review is dangerous in production. A bad policy can break inter-service communication cluster-wide. Violates GitOps principles. | Generate YAML files for human review. Let users apply via their existing GitOps pipeline (ArgoCD, Flux). |
| JSON file/stdin input mode | "I want to process historical Hubble exports" | Doubles the input surface area. JSON flow format differs from proto types — requires a separate parser and type mapping. Hubble JSON format is not a stable API. | gRPC only. Users can re-observe historical flows via Hubble's time-range filters if supported, or simply re-run the tool. |
| L7 policy generation (HTTP paths, DNS names) | "I want application-layer policies" | L7 policies require Envoy proxy injection, dramatically increase complexity, and have different performance characteristics. L7 flow data availability depends on Cilium configuration. Two-step process (deploy L4 first, then observe L7). | Start with L3/L4 only. L7 is a potential v2+ feature with significant architecture implications. |
| CiliumClusterwideNetworkPolicy generation | "I want cluster-scoped policies" | Different CRD, different semantics (no namespace selector), different RBAC implications. Mixing namespace and cluster-wide generation increases complexity without clear UX benefit. | Namespace-scoped only. Cluster-wide policies are typically hand-crafted by platform teams, not generated. |
| Web UI / dashboard | "I want to visualize generated policies" | Massive scope increase. networkpolicy.io already provides excellent visualization. Building a UI is a different product. | Output YAML that can be pasted into editor.networkpolicy.io for visualization. |
| Policy simulation / dry-run | "Show me what would happen if I apply this policy" | Cilium already has policy audit mode for this exact purpose. Reimplementing simulation is duplicating Cilium functionality with lower fidelity. | Document the audit-mode workflow: generate policy, apply in audit mode, observe with Hubble, then enforce. |
| Named port resolution | "Use service port names instead of numbers" | Requires querying the Kubernetes API for Service definitions, adds a dependency, and introduces ambiguity (same port name can map to different numbers across services). | Use exact port numbers from flows. Unambiguous and matches what the datapath actually sees. |

## Feature Dependencies

```
[Hubble gRPC Connection]
    ├──requires──> [Auto Port-Forward] (for zero-config UX)
    ├──enables──> [Flow Filtering by Namespace/Labels]
    └──enables──> [Real-time Streaming]
                      └──enables──> [Policy Generation (Ingress)]
                      └──enables──> [Policy Generation (Egress)]
                      └──enables──> [CIDR Policy Generation]
                                        └──all require──> [Label Selection Heuristics]
                                        └──all require──> [Port/Protocol Extraction]
                                        └──all produce──> [YAML Output]
                                                              └──consumed by──> [File Deduplication]
                                                              └──consumed by──> [Cluster Deduplication]

[Cluster Deduplication] ──requires──> [K8s Client (client-go)]
[Auto Port-Forward] ──requires──> [K8s Client (client-go)]
```

### Dependency Notes

- **Policy Generation requires Hubble Connection:** No flows = no policies. The gRPC connection is the foundation.
- **Auto Port-Forward requires client-go:** Must discover and port-forward to hubble-relay service. Shares the K8s client with cluster dedup.
- **Cluster Dedup requires client-go:** Must list existing CiliumNetworkPolicies. Same client as port-forward.
- **File Dedup and Cluster Dedup are independent:** Can implement either without the other, but both are needed for a complete solution.
- **Label Selection is cross-cutting:** Used by all policy generation (ingress, egress, CIDR). Must be implemented early.

## MVP Definition

### Launch With (v1)

Minimum viable product — what's needed to validate the core concept of "flows to policies."

- [ ] Hubble Relay gRPC connection with `--server` flag — foundational connectivity
- [ ] Observe dropped flows filtered by namespace — scoped input
- [ ] Generate ingress CiliumNetworkPolicy from dropped flows — core value proposition
- [ ] Generate egress CiliumNetworkPolicy from dropped flows — complete directional coverage
- [ ] Label-based endpoint selectors with smart defaults — usable policies
- [ ] Exact port + protocol in generated rules — security-correct output
- [ ] Valid YAML output to stdout or file — minimum output path
- [ ] Structured logging with zap — debuggability from day one

### Add After Validation (v1.x)

Features to add once core generation is proven correct and useful.

- [ ] Auto port-forward to Hubble Relay — improves UX, removes manual setup step
- [ ] CIDR-based policies for external traffic — covers non-cluster destinations
- [ ] One file per policy in organized directory structure — GitOps-friendly output
- [ ] File-based deduplication (against output directory) — prevents duplicates in continuous mode
- [ ] Real-time continuous streaming mode — long-running observation sessions
- [ ] Cluster-aware deduplication via client-go — prevents duplicates of already-applied policies

### Future Consideration (v2+)

Features to defer until the tool has proven utility.

- [ ] L7 policy generation (HTTP, DNS) — requires significant architecture changes, Envoy dependency
- [ ] Policy merging / consolidation — combine multiple granular policies into fewer, broader ones
- [ ] Integration with Cilium policy audit mode — automated audit-then-enforce workflow
- [ ] Prometheus metrics for the tool itself — operational monitoring of long-running instances
- [ ] Policy diff against existing cluster state — show what would change

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Hubble gRPC connection | HIGH | MEDIUM | P1 |
| Namespace flow filtering | HIGH | LOW | P1 |
| Ingress policy generation | HIGH | MEDIUM | P1 |
| Egress policy generation | HIGH | MEDIUM | P1 |
| Smart label selection | HIGH | MEDIUM | P1 |
| Port/protocol extraction | HIGH | LOW | P1 |
| YAML output (valid CNP) | HIGH | LOW | P1 |
| Structured logging (zap) | MEDIUM | LOW | P1 |
| Auto port-forward | HIGH | MEDIUM | P2 |
| CIDR policies (external traffic) | HIGH | MEDIUM | P2 |
| Organized file output | MEDIUM | LOW | P2 |
| File deduplication | MEDIUM | MEDIUM | P2 |
| Real-time streaming | MEDIUM | HIGH | P2 |
| Cluster deduplication | MEDIUM | HIGH | P2 |
| L7 policy generation | LOW | HIGH | P3 |
| Policy merging | LOW | HIGH | P3 |
| Audit mode integration | LOW | MEDIUM | P3 |

**Priority key:**
- P1: Must have for launch
- P2: Should have, add when possible
- P3: Nice to have, future consideration

## Competitor Feature Analysis

| Feature | siegmund-heiss-ich/cnp-generator (Python) | Inspektor Gadget Network Policy Advisor | ARMO Platform | Calico Enterprise | CPG (our approach) |
|---------|------------------------------------------|----------------------------------------|---------------|-------------------|--------------------|
| Input source | Exported Hubble JSON files | eBPF tracing (kernel-level) | eBPF runtime monitoring | Flow logs (Calico dataplane) | Live Hubble Relay gRPC stream |
| Output format | CiliumNetworkPolicy YAML | Standard Kubernetes NetworkPolicy YAML | Standard Kubernetes NetworkPolicy | Calico NetworkPolicy | CiliumNetworkPolicy YAML |
| Real-time | No (batch on exports) | No (record then report) | Yes (24h learning phase) | Yes (continuous) | Yes (streaming) |
| L3/L4 support | Yes | Yes | Yes | Yes | Yes |
| L7 support | Yes (two-step process) | No | No | Yes | No (v1) |
| CIDR/external | Yes | Limited | Yes (IP enrichment) | Yes | Yes |
| Dedup against cluster | No | No | Yes (platform-managed) | Yes (platform-managed) | Yes |
| Deployment model | CLI (offline) | kubectl plugin (in-cluster) | SaaS platform | Commercial platform | CLI (live connection) |
| Language | Python | Go | Proprietary | Proprietary | Go |
| Cost | Free/OSS | Free/OSS | Commercial | Commercial | Free/OSS |
| Cilium-native types | No (constructs YAML) | No (standard K8s NP) | No (standard K8s NP) | N/A (Calico NP) | Yes (Go CRD types) |

### Key Competitive Insights

1. **No direct OSS competitor does live gRPC streaming to Hubble Relay.** The Python tool works on exports; Inspektor Gadget uses its own eBPF hooks. This is CPG's primary differentiator.
2. **Commercial platforms (ARMO, Calico Enterprise) have the richest feature sets** but are SaaS/commercial. CPG targets the OSS CLI niche.
3. **Inspektor Gadget generates standard K8s NetworkPolicy, not CiliumNetworkPolicy.** Users running Cilium lose access to Cilium-specific features (CIDR, entity selectors, L7).
4. **The existing Python generator is the closest competitor** but requires a two-step export workflow and has no cluster-aware dedup.

## Sources

- [siegmund-heiss-ich/cilium-network-policy-generator](https://github.com/siegmund-heiss-ich/cilium-network-policy-generator) — Python-based, works on exported Hubble flows
- [Inspektor Gadget Network Policy Advisor](https://inspektor-gadget.io/blog/2020/03/writing-kubernetes-network-policies-with-inspektor-gadgets-network-policy-advisor/) — eBPF-based K8s NetworkPolicy generation
- [ARMO Platform Network Policy](https://www.armosec.io/blog/kubernetes-network-policy-future/) — Commercial eBPF-based auto-generation
- [Calico Enterprise Policy Recommendation](https://www.tigera.io/blog/security-policy-as-code-now-fully-automated-with-calico-enterprise-2-6/) — Commercial flow-based policy generation
- [Cilium Creating Policies from Verdicts](https://docs.cilium.io/en/stable/security/policy-creation/) — Official Cilium audit-mode workflow
- [NetworkPolicy Editor](https://editor.networkpolicy.io/) — Cilium community visualization tool
- [Cilium Policy Audit Mode](https://github.com/cilium/cilium/issues/9580) — Audit mode design and implementation
- [Cilium 1.18 Policy Log Field](https://blog.zwindler.fr/en/2025/11/03/ciliums-new-policy-log-field-our-use-case/) — New policy logging features

---
*Feature research for: Cilium network policy generation CLI tools*
*Researched: 2026-03-08*
