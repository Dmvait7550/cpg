package policy_test

import (
	"testing"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gule/cpg/pkg/policy"
)

func makePolicy(name string, ingress []api.IngressRule, egress []api.EgressRule) *ciliumv2.CiliumNetworkPolicy {
	return &ciliumv2.CiliumNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: &api.Rule{
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func TestPoliciesEquivalent_SameSpecDifferentMeta(t *testing.T) {
	ingress := []api.IngressRule{
		{
			IngressCommonRule: api.IngressCommonRule{
				FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")},
			},
			ToPorts: api.PortRules{
				{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
			},
		},
	}

	a := makePolicy("policy-a", ingress, nil)
	b := makePolicy("policy-b", ingress, nil)

	assert.True(t, policy.PoliciesEquivalent(a, b), "same spec with different metadata should be equivalent")
}

func TestPoliciesEquivalent_DifferentIngressRules(t *testing.T) {
	a := makePolicy("policy", []api.IngressRule{
		{
			IngressCommonRule: api.IngressCommonRule{
				FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")},
			},
			ToPorts: api.PortRules{
				{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
			},
		},
	}, nil)

	b := makePolicy("policy", []api.IngressRule{
		{
			IngressCommonRule: api.IngressCommonRule{
				FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.2/32")},
			},
			ToPorts: api.PortRules{
				{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
			},
		},
	}, nil)

	assert.False(t, policy.PoliciesEquivalent(a, b), "different ingress rules should not be equivalent")
}

func TestPoliciesEquivalent_DifferentOrderSameRules(t *testing.T) {
	ruleA := api.IngressRule{
		IngressCommonRule: api.IngressCommonRule{
			FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")},
		},
		ToPorts: api.PortRules{
			{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
		},
	}
	ruleB := api.IngressRule{
		IngressCommonRule: api.IngressCommonRule{
			FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.2/32")},
		},
		ToPorts: api.PortRules{
			{Ports: []api.PortProtocol{{Port: "443", Protocol: api.ProtoTCP}}},
		},
	}

	a := makePolicy("policy", []api.IngressRule{ruleA, ruleB}, nil)
	b := makePolicy("policy", []api.IngressRule{ruleB, ruleA}, nil)

	assert.True(t, policy.PoliciesEquivalent(a, b), "same rules in different order should be equivalent")
}

func TestPoliciesEquivalent_NilPolicy(t *testing.T) {
	p := makePolicy("policy", nil, nil)

	assert.False(t, policy.PoliciesEquivalent(p, nil), "policy vs nil should not be equivalent")
	assert.False(t, policy.PoliciesEquivalent(nil, p), "nil vs policy should not be equivalent")
	assert.True(t, policy.PoliciesEquivalent(nil, nil), "nil vs nil should be equivalent")
}
