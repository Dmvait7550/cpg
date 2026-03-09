package policy_test

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/gule/cpg/pkg/policy"
	"github.com/gule/cpg/pkg/policy/testdata"
)

// TestPoliciesEquivalent_TwoBuildPolicyOutputs verifies that two identical
// BuildPolicy calls produce equivalent policies.
func TestPoliciesEquivalent_TwoBuildPolicyOutputs(t *testing.T) {
	flows := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 80),
		testdata.IngressTCPFlow([]string{"k8s:app=other"}, []string{"k8s:app=server"}, "default", 443),
		testdata.WorldIngressTCPFlow("1.2.3.4", 8080, []string{"k8s:app=server"}, "default"),
	}

	p1 := policy.BuildPolicy("default", "server", flows)
	p2 := policy.BuildPolicy("default", "server", flows)

	assert.True(t, policy.PoliciesEquivalent(p1, p2),
		"Two BuildPolicy outputs from same flows should be equivalent")
}

// TestPoliciesEquivalent_BuildPolicyVsYAMLRoundtrip verifies that a
// BuildPolicy output is equivalent to itself after YAML marshal/unmarshal.
// This is critical for the cross-flush dedup in the pipeline.
func TestPoliciesEquivalent_BuildPolicyVsYAMLRoundtrip(t *testing.T) {
	flows := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 80),
		testdata.WorldIngressTCPFlow("1.2.3.4", 443, []string{"k8s:app=server"}, "default"),
		testdata.WorldEgressTCPFlow([]string{"k8s:app=server"}, "default", "8.8.8.8", 53),
	}

	original := policy.BuildPolicy("default", "server", flows)

	data, err := yaml.Marshal(original)
	require.NoError(t, err)
	t.Logf("YAML:\n%s", data)

	var roundtripped ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(data, &roundtripped))

	equiv := policy.PoliciesEquivalent(original, &roundtripped)
	if !equiv {
		t.Logf("Original ingress: %+v", original.Spec.Ingress)
		t.Logf("Roundtripped ingress: %+v", roundtripped.Spec.Ingress)
		t.Logf("Original egress: %+v", original.Spec.Egress)
		t.Logf("Roundtripped egress: %+v", roundtripped.Spec.Egress)
		t.Logf("Original endpoint selector: %+v", original.Spec.EndpointSelector)
		t.Logf("Roundtripped endpoint selector: %+v", roundtripped.Spec.EndpointSelector)
	}
	assert.True(t, equiv,
		"BuildPolicy output should be equivalent to its YAML-roundtripped version")
}

// TestPoliciesEquivalent_MergedVsOriginal verifies that merging identical
// content into an existing policy produces an equivalent result.
func TestPoliciesEquivalent_MergedVsOriginal(t *testing.T) {
	flows := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 80),
	}

	original := policy.BuildPolicy("default", "server", flows)

	// Roundtrip
	data, err := yaml.Marshal(original)
	require.NoError(t, err)
	var fromDisk ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(data, &fromDisk))

	// Merge same content
	incoming := policy.BuildPolicy("default", "server", flows)
	merged := policy.MergePolicy(&fromDisk, incoming)

	assert.True(t, policy.PoliciesEquivalent(original, merged),
		"Merging identical content should produce equivalent policy")
}
