package policy

import (
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
)

// MergePolicy merges incoming policy rules into an existing policy.
// It adds new ports to existing rules for the same peer, and appends
// new peers as separate rules.
func MergePolicy(existing, incoming *ciliumv2.CiliumNetworkPolicy) *ciliumv2.CiliumNetworkPolicy {
	// TODO: implement
	return existing
}
