package k8s

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// Register auth provider plugins (OIDC, GCP, Azure, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// LoadKubeConfig loads a Kubernetes rest.Config from the standard kubeconfig
// locations (KUBECONFIG env, ~/.kube/config, in-cluster).
func LoadKubeConfig() (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil)

	cfg, err := config.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	return cfg, nil
}
