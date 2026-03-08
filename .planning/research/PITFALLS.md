# Pitfalls Research

**Domain:** Hubble gRPC integration and CiliumNetworkPolicy generation CLI tool
**Researched:** 2026-03-08
**Confidence:** HIGH (verified against official Cilium docs, Datadog post-mortem, Hubble issue tracker)

## Critical Pitfalls

### Pitfall 1: Traffic Direction Inversion When Generating Policies

**What goes wrong:**
Hubble flows have a `traffic_direction` field (INGRESS/EGRESS) that describes the direction *from the perspective of the observing endpoint*. A dropped EGRESS flow on pod A means A tried to send traffic out. The generated policy needs an egress rule on A's policy *and* potentially an ingress rule on the destination's policy. Naive generators create the rule on only one side, or worse, invert ingress/egress by confusing source/destination roles with traffic direction.

**Why it happens:**
The flow's `source` and `destination` fields describe the actual sender/receiver, but `traffic_direction` describes which endpoint reported the drop. A single connection attempt can produce two dropped flows (one EGRESS on the source, one INGRESS on the destination). Developers conflate "source = needs egress rule" without considering which endpoint the policy targets (`endpointSelector`).

**How to avoid:**
- Always anchor policy generation to the `endpointSelector` (the pod the policy applies to)
- If `traffic_direction == EGRESS`: the selected endpoint is the source, generate an egress rule with `toEndpoints`/`toCIDR` for the destination
- If `traffic_direction == INGRESS`: the selected endpoint is the destination, generate an ingress rule with `fromEndpoints`/`fromCIDR` for the source
- Write explicit unit tests with flows from both perspectives of the same connection

**Warning signs:**
- Generated policies that have ingress rules using `toEndpoints` (wrong field)
- Policies that "don't work" even though flows were observed
- Duplicate/conflicting policies generated for the same traffic pair

**Phase to address:**
Core policy generation phase (earliest). This is foundational logic that everything else depends on.

---

### Pitfall 2: CIDR Rules Applied to Cilium-Managed Endpoints

**What goes wrong:**
When a dropped flow involves a pod whose IP is known but whose identity/labels are not resolved (e.g., the flow lacks labels for the remote side), a naive generator falls back to CIDR-based rules using the pod's IP. Cilium explicitly documents that CIDR rules do not apply to traffic where both sides are managed by Cilium or use a node IP. The generated policy silently does nothing.

**Why it happens:**
Hubble flows for cross-namespace or cross-node traffic sometimes have incomplete label data on the remote endpoint. The temptation is to use the IP address as a fallback. But Cilium resolves managed endpoints by security identity, not IP, so CIDR rules are ignored for managed traffic.

**How to avoid:**
- Only generate CIDR-based rules (`toCIDR`/`fromCIDR`) when the remote endpoint has the `reserved:world` identity or is explicitly unmanaged
- Check the flow's `source.identity` / `destination.identity` fields: reserved identities like `WORLD` (numeric 2) indicate external traffic suitable for CIDR
- For managed endpoints with missing labels, use the `reserved:cluster` entity or resolve labels from the cluster via client-go rather than falling back to IP
- Never generate `toCIDR` with a pod CIDR range

**Warning signs:**
- Generated policies with `/32` CIDR rules pointing at pod IPs within the cluster
- Policies that pass YAML validation but have no effect when applied
- Users reporting "I applied the policy but traffic is still denied"

**Phase to address:**
Core policy generation phase. Must be correct before CIDR policy support is added.

---

### Pitfall 3: Namespace Scoping and Cross-Namespace Label Selection

**What goes wrong:**
CiliumNetworkPolicy is namespace-scoped. The `endpointSelector` always and only matches pods in the policy's own namespace. When generating `fromEndpoints` or `toEndpoints` rules for cross-namespace traffic, omitting the `k8s:io.kubernetes.pod.namespace` label causes the selector to match pods in the policy's namespace instead of the intended remote namespace. This silently creates wrong rules.

**Why it happens:**
Kubernetes and Cilium handle namespace scoping differently than most developers expect. The `endpointSelector` is implicitly scoped, but `fromEndpoints`/`toEndpoints` require explicit namespace labels. This is not intuitive and the Cilium docs bury this detail.

**How to avoid:**
- Always include `k8s:io.kubernetes.pod.namespace: <namespace>` in `fromEndpoints`/`toEndpoints` selectors when the remote pod is in a different namespace than the policy
- Extract namespace from the flow's `source.namespace` / `destination.namespace` fields
- Validate generated policies: if policy namespace differs from the remote endpoint namespace in the selector, the namespace label MUST be present

**Warning signs:**
- Cross-namespace policies that "work" only because they accidentally match a same-namespace pod with similar labels
- Integration tests passing with single-namespace setups but failing multi-namespace

**Phase to address:**
Core policy generation phase, with dedicated cross-namespace test cases.

---

### Pitfall 4: Hubble Ring Buffer Overflow Causing Incomplete Flow Data

**What goes wrong:**
Hubble stores flows in a fixed-size ring buffer per node (default 4096 events). In high-traffic clusters, the buffer overflows and older events are lost. The tool observes `LostEvent` messages instead of actual flows. Generated policies are incomplete because not all denied traffic was captured, giving users false confidence that their policy set is complete.

**Why it happens:**
The ring buffer is intentionally small to limit memory usage. Production clusters with default-deny can generate thousands of drops per second during initial policy rollout. The tool has no way to know what it missed.

**How to avoid:**
- Handle `LostEvent` responses in the gRPC stream explicitly: log warnings with the count of lost events
- Surface lost event counts to the user prominently (not buried in debug logs)
- Document that users should increase `hubble-event-buffer-capacity` if they see lost events
- Consider recommending users filter by namespace to reduce flow volume
- Never claim "all policies generated" if lost events were observed

**Warning signs:**
- `LostEvent` messages in the gRPC stream response
- Suspiciously few policies generated for a namespace with many services
- Policies work for some services but not others in the same namespace

**Phase to address:**
Hubble connection/streaming phase. Must handle LostEvents from the first version.

---

### Pitfall 5: Importing github.com/cilium/cilium as a Go Dependency

**What goes wrong:**
The `github.com/cilium/cilium` module is enormous (the agent binary alone is ~80 MiB). Importing it for the observer proto types and CRD types pulls in hundreds of transitive dependencies, massively inflates build time and binary size, and creates version conflicts with `client-go` and other K8s libraries.

**Why it happens:**
Cilium does not publish its proto types or CRD types as separate Go modules. The entire monorepo is one Go module. Any import from `pkg/k8s/apis/cilium.io/v2` or `api/v1/observer` transitively depends on large parts of the codebase.

**How to avoid:**
- Pin the exact Cilium version matching the target cluster version
- Use `go mod tidy` aggressively and check binary size after each dependency addition
- Consider generating proto types from the `.proto` files directly (`buf generate`) instead of importing the Cilium module for observer types
- For CRD types (`CiliumNetworkPolicy`), evaluate whether generating YAML directly (without CRD Go types) is sufficient since the output is YAML files, not API calls
- If importing the Cilium module, use Go build tags and careful package isolation to minimize transitive deps
- Set up CI to fail if binary exceeds a size threshold (e.g., 50 MiB)

**Warning signs:**
- `go mod download` taking >2 minutes
- Binary size >40 MiB for a CLI tool
- Version conflicts between cilium/cilium's pinned `client-go` and the project's `client-go`
- Build failures after Cilium version bumps

**Phase to address:**
Project scaffolding / initial setup. This decision propagates through the entire project.

---

### Pitfall 6: Generating Overly Permissive Policies from Insufficient Flow Data

**What goes wrong:**
If the tool generates policies from a short observation window, it captures only the traffic patterns active during that period. The generated policies may be too narrow (missing legitimate traffic) or, worse, too broad if the generator aggregates selectors loosely (e.g., allowing all pods with `app: frontend` when only one specific pod needed access).

**Why it happens:**
Network traffic is dynamic: batch jobs, cron schedules, scaling events, deployments, and health checks all create traffic patterns that may not appear during a short observation. The existing `cilium-network-policy-generator` project explicitly warns "it should work fine, as long as there is enough data."

**How to avoid:**
- Never claim generated policies are "complete" -- always frame them as "based on observed traffic"
- Add timestamps to generated policy files showing the observation window
- In streaming mode, accumulate and merge policies over time rather than generating point-in-time snapshots
- Document that users should observe during peak traffic / full application lifecycle

**Warning signs:**
- Policies generated from <5 minutes of observation
- Missing rules for known periodic traffic (health checks, metrics scraping)
- Users applying generated policies and immediately seeing new drops

**Phase to address:**
Policy generation and deduplication phase. The streaming/accumulation design must account for this.

---

### Pitfall 7: toServices and toPorts Combination Silently Ignored

**What goes wrong:**
When generating egress rules, combining `toServices` with `toPorts` in the same rule causes Cilium to silently ignore the `toServices` clause. The resulting policy allows traffic to ANY destination on those ports instead of restricting to the named service.

**Why it happens:**
This is an undocumented (until Datadog's blog) Cilium behavior. The YAML is valid, the policy applies without errors, but the semantics are wrong. A policy generator that tries to be "helpful" by adding both service reference and port restriction creates a security hole.

**How to avoid:**
- Never generate rules combining `toServices` and `toPorts` in the same egress rule
- Use `toEndpoints` with label selectors + `toPorts` instead of `toServices`
- Since the tool uses flow data (which contains labels and ports, not service names), this is naturally avoided by generating endpoint-based rules

**Warning signs:**
- Generated egress rules containing both `toServices` and `toPorts` fields
- Security audits showing pods can reach unintended destinations on allowed ports

**Phase to address:**
Policy generation phase. Validate generated YAML structure against known Cilium quirks.

---

### Pitfall 8: Port-Forward and TLS/ALPN Handshake Failures

**What goes wrong:**
When using `kubectl port-forward` to reach Hubble Relay, newer gRPC versions expect ALPN/h2 during TLS handshake. Hubble Relay does not fully support this, causing connection failures with cryptic TLS errors even when certificates are correct.

**Why it happens:**
gRPC-Go has evolved its TLS requirements. The `grpc-go` version pulled by the Cilium module may differ from what the project uses directly, creating version-dependent TLS behavior. Port-forwarding adds another layer of protocol complexity.

**How to avoid:**
- Test port-forward connectivity early in development with the exact gRPC and TLS configuration
- Use `grpc.WithTransportCredentials(insecure.NewCredentials())` when connecting through port-forward (the port-forward tunnel is already authenticated by kubectl)
- If TLS is required, explicitly configure `tls.Config` with `NextProtos: []string{"h2"}` or disable ALPN
- Document the TLS configuration clearly for users
- Consider supporting both TLS and non-TLS modes with a `--tls` / `--insecure` flag

**Warning signs:**
- "TLS handshake failure" errors during port-forward connections
- Connection works with `hubble` CLI but not with the tool
- Intermittent connection failures after gRPC library upgrades

**Phase to address:**
Hubble connection phase. Must be validated before streaming can work.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Generating raw YAML strings instead of using CRD Go types | Avoids massive Cilium dependency | No type safety, easy to generate invalid YAML, hard to validate | MVP only, migrate to structured generation later |
| Hardcoded label priority (always use `app` label) | Simple implementation | Misses workloads using different labeling conventions | Never -- must inspect actual flow labels |
| Skipping LostEvent handling | Faster initial implementation | Silent data loss, incomplete policies | Never -- even a warning log is better than silence |
| Single-pass policy generation (no accumulation) | Simpler streaming logic | Misses periodic traffic, generates incomplete policies | MVP with clear documentation of limitation |
| No cluster dedup (file-only dedup) | Avoids client-go dependency | Generates policies that already exist in cluster | Early phases, but cluster dedup should come soon |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Hubble Relay gRPC | Using `--since` and `--follow` together (not supported) | Use `--follow` only for streaming, `--since` only for historical queries |
| Hubble Relay gRPC | Not handling stream reconnection after relay pod restart | Implement retry with exponential backoff using `WithRetryTimeout` pattern |
| Hubble Relay gRPC | Assuming all nodes' flows come through one relay connection | Relay fans out to all peers; be aware of partial results during peer connection issues |
| client-go CRD listing | Using dynamic client without proper CRD type registration | Register CiliumNetworkPolicy types with the scheme, or use the Cilium client package |
| kubectl port-forward | Assuming port-forward is stable for long-running streams | Port-forward connections drop; implement reconnection logic with state preservation |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Unbounded in-memory policy accumulation | OOM after hours of streaming in large clusters | Flush to disk periodically, use bounded maps with LRU eviction | >10K unique flow pairs observed |
| Per-flow file I/O for dedup checking | High latency, I/O bottleneck | Load existing policies into memory at startup, update incrementally | >500 existing policy files |
| Synchronous cluster API calls per flow | gRPC stream backpressure, dropped events | Batch cluster queries, cache CiliumNetworkPolicy list with periodic refresh | >100 flows/second |
| String-based YAML comparison for dedup | False negatives from field ordering differences | Compare structured policy objects, not YAML strings | Any non-trivial usage |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Generating `fromEntities: [world]` when a CIDR range would suffice | Allows all external traffic instead of specific sources | Map `reserved:world` identity to specific CIDR when IP is available in flow |
| Not sanitizing label values from flows | Label injection if flow data is crafted | Validate label key/value format against K8s label constraints before using in policy |
| Generating policies with empty `endpointSelector` | Policy applies to ALL pods in namespace | Always populate `endpointSelector` with at least one label from the target workload |
| Storing kubeconfig credentials in memory longer than needed | Credential exposure if process is compromised | Use client-go's default credential chain, don't cache tokens |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| No progress indicator during flow observation | User thinks tool is hung when no drops occur | Show "Listening for dropped flows..." with flow count, even if zero policies generated |
| Generating one policy per flow instead of merging | Hundreds of tiny policy files | Merge rules for the same `endpointSelector` into a single policy file |
| No human-readable policy naming | Files like `policy-abc123.yaml` are unreadable | Name files `<namespace>-<workload>-<direction>.yaml` |
| Silent overwrite of existing policy files | User loses manually-tuned policies | Never overwrite; skip with warning, or use `--force` flag |
| No summary output after generation | User doesn't know what was generated | Print summary: "Generated 5 ingress, 3 egress, 2 CIDR policies for namespace X" |

## "Looks Done But Isn't" Checklist

- [ ] **Ingress policy generation:** Often missing cross-namespace `k8s:io.kubernetes.pod.namespace` label -- verify with multi-namespace test
- [ ] **Egress policy generation:** Often missing DNS egress rules (pods need to resolve service names) -- verify kube-dns/coredns access is included
- [ ] **CIDR policies:** Often using pod CIDRs instead of external IPs -- verify no managed-endpoint IPs appear in CIDR rules
- [ ] **Port specification:** Often missing protocol field (defaults to TCP, breaking UDP traffic) -- verify protocol is always set from flow data
- [ ] **Deduplication:** Often comparing YAML strings instead of semantic equality -- verify field ordering doesn't create false duplicates
- [ ] **Policy naming:** Often forgetting that K8s resource names must be DNS-compatible -- verify generated names pass validation
- [ ] **Streaming reconnection:** Often assuming the gRPC stream stays open forever -- verify behavior after relay pod restart

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Wrong direction (ingress/egress inversion) | MEDIUM | Delete generated policies, fix direction logic, regenerate. No data loss but wasted time. |
| CIDR rules on managed endpoints | LOW | Delete ineffective policies, regenerate with identity check. Policies had no effect anyway. |
| Namespace scoping error | HIGH | Audit all generated cross-namespace policies. May have created security holes if wrong namespace matched. |
| Incomplete data from lost events | LOW | Re-run observation with larger buffer or narrower filter. Additive process. |
| Cilium module dependency hell | HIGH | Major refactor to extract proto generation or switch to YAML templating. Affects entire build. |
| Overly permissive entity rules | HIGH | Security audit required. Replace with specific endpoint selectors. May require flow re-observation. |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Traffic direction inversion | Core policy generation | Unit tests with bidirectional flow pairs produce correct ingress/egress rules |
| CIDR on managed endpoints | Core policy generation | No `/32` pod-CIDR rules in output; only `reserved:world` flows produce CIDR rules |
| Namespace scoping | Core policy generation | Multi-namespace integration test generates correct `k8s:io.kubernetes.pod.namespace` labels |
| Ring buffer overflow | Hubble streaming | LostEvent count logged and surfaced in CLI output; test with mock LostEvent responses |
| Cilium Go module size | Project scaffolding | CI gate on binary size; evaluate proto-only generation approach before importing full module |
| Insufficient flow data | Policy generation + UX | Observation window documented in generated files; user warned about short windows |
| toServices + toPorts | Policy generation | Linter/validator rejects this combination in generated output |
| Port-forward TLS/ALPN | Hubble connection | Connection tested with both direct and port-forwarded relay; TLS mode configurable |

## Sources

- [Datadog: CiliumNetworkPolicy Misconfigurations](https://www.datadoghq.com/blog/cilium-network-policy-misconfigurations/) (December 2025)
- [Cilium Layer 3 Policy Documentation](https://docs.cilium.io/en/stable/security/policy/language/)
- [Cilium Policy Enforcement Modes](https://docs.cilium.io/en/stable/security/policy/intro/)
- [Hubble Internals Documentation](https://docs.cilium.io/en/stable/internals/hubble/)
- [Hubble Ring Buffer Lost Events Issue #15112](https://github.com/cilium/cilium/issues/15112)
- [Hubble Ring Buffer Events Lost Issue #1737](https://github.com/cilium/hubble/issues/1737)
- [Hubble GetFlows --since and --follow Incompatibility Issue #363](https://github.com/cilium/hubble/issues/363)
- [Hubble Relay gRPC Connection Timeout Issue #12645](https://github.com/cilium/cilium/issues/12645)
- [Hubble Relay Timeout Issue #36979](https://github.com/cilium/cilium/issues/36979)
- [CiliumNetworkPolicy Namespace Label Issue #30149](https://github.com/cilium/cilium/issues/30149)
- [Cilium Thinning Binaries Issue #25980](https://github.com/cilium/cilium/issues/25980)
- [Endpoint Selectors and Kubernetes Namespaces in CNPs (Scott Lowe)](https://blog.scottlowe.org/2024/05/30/endpoint-selectors-and-kubernetes-namespaces-in-ciliumnetworkpolicies/)
- [siegmund-heiss-ich/cilium-network-policy-generator](https://github.com/siegmund-heiss-ich/cilium-network-policy-generator)
- [CNCF: Safely Managing Cilium Network Policies](https://www.cncf.io/blog/2025/11/06/safely-managing-cilium-network-policies-in-kubernetes-testing-and-simulation-techniques/)
- [CIDR Policy and Managed Endpoints Issue #23603](https://github.com/cilium/cilium/issues/23603)

---
*Pitfalls research for: Cilium Policy Generator (CPG) -- Hubble gRPC + CiliumNetworkPolicy generation*
*Researched: 2026-03-08*
