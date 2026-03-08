---
phase: 3
slug: production-hardening
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-03-08
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none — standard Go testing |
| **Quick run command** | `go test ./pkg/... ./cmd/...` |
| **Full suite command** | `go test -race -count=1 ./...` |
| **Estimated runtime** | ~5 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/... ./cmd/...`
- **After every plan wave:** Run `go test -race -count=1 ./...`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 03-01-01 | 01 | 1 | CONN-02 | unit+integration | `go test ./pkg/hubble/ -run TestPortForward` | ❌ W0 | ⬜ pending |
| 03-01-02 | 01 | 1 | DEDP-01 | unit | `go test ./pkg/output/ -run TestDedup` | ❌ W0 | ⬜ pending |
| 03-02-01 | 02 | 2 | DEDP-02 | unit | `go test ./pkg/dedup/ -run TestCluster` | ❌ W0 | ⬜ pending |
| 03-02-02 | 02 | 2 | DEDP-03 | unit | `go test ./pkg/hubble/ -run TestAggregat` | ✅ | ⬜ pending |
| 03-02-03 | 02 | 2 | PGEN-03 | unit | `go test ./pkg/policy/ -run TestCIDR` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/hubble/portforward_test.go` — stubs for CONN-02 port-forward
- [ ] `pkg/output/writer_test.go` — extend with dedup stubs for DEDP-01
- [ ] `pkg/dedup/cluster_test.go` — stubs for DEDP-02 cluster dedup
- [ ] `pkg/policy/builder_test.go` — extend with CIDR stubs for PGEN-03

*Existing infrastructure covers DEDP-03 (aggregator tests exist from Phase 2).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Auto port-forward to live cluster | CONN-02 | Requires real Kubernetes cluster | Run `cpg generate` without `--server`, verify connection to hubble-relay |
| Cluster dedup against live CiliumNetworkPolicies | DEDP-02 | Requires Cilium CRDs in cluster | Apply a CNP, run generate, verify skip |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
