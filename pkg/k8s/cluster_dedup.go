package k8s

import (
	"context"
	"fmt"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	ciliumclient "github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// ManagedByLabel is the label selector used to find CPG-managed policies.
const ManagedByLabel = "app.kubernetes.io/managed-by=cpg"

// LoadClusterPolicies lists CiliumNetworkPolicies with the managed-by=cpg label
// from the given namespace and returns them as a map keyed by policy name.
// This is used for cluster-based deduplication (opt-in via --cluster-dedup).
func LoadClusterPolicies(ctx context.Context, config *rest.Config, namespace string) (map[string]*ciliumv2.CiliumNetworkPolicy, error) {
	cs, err := ciliumclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating Cilium clientset: %w", err)
	}

	list, err := cs.CiliumV2().CiliumNetworkPolicies(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ManagedByLabel,
	})
	if err != nil {
		return nil, fmt.Errorf("listing CiliumNetworkPolicies in %s: %w", namespace, err)
	}

	return buildClusterPolicyMap(list.Items), nil
}

// buildClusterPolicyMap converts a slice of CNPs into a map keyed by policy name.
func buildClusterPolicyMap(policies []ciliumv2.CiliumNetworkPolicy) map[string]*ciliumv2.CiliumNetworkPolicy {
	result := make(map[string]*ciliumv2.CiliumNetworkPolicy, len(policies))
	for i := range policies {
		result[policies[i].Name] = &policies[i]
	}
	return result
}
