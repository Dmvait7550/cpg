package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func newGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate CiliumNetworkPolicies from Hubble flow observations",
		Long: `Connect to Hubble Relay via gRPC, stream dropped flows, and generate
CiliumNetworkPolicy YAML files organized by namespace and workload.

Runs continuously until interrupted (Ctrl+C). On shutdown, flushes all
accumulated flows and displays a session summary.

Output files are organized as <output-dir>/<namespace>/<workload>.yaml.
When a policy file already exists, new rules are merged into the existing
policy (ports are deduplicated, new peers are appended).

Examples:
  # Generate policies from local Hubble Relay
  cpg generate --server localhost:4245

  # Filter to specific namespaces
  cpg generate --server relay.example.com:443 --tls -n production -n staging

  # All namespaces with debug logging
  cpg --debug generate --server localhost:4245 --all-namespaces`,
		RunE: runGenerate,
	}

	// Connection flags
	cmd.Flags().StringP("server", "s", "", "Hubble Relay address (required)")
	cmd.Flags().BoolP("tls", "", false, "enable TLS for gRPC connection")
	cmd.Flags().Duration("timeout", 10*time.Second, "connection timeout")

	// Namespace filtering
	cmd.Flags().StringSliceP("namespace", "n", nil, "namespace filter (repeatable)")
	cmd.Flags().BoolP("all-namespaces", "A", false, "observe all namespaces")

	// Output
	cmd.Flags().StringP("output-dir", "o", "./policies", "output directory for generated policies")

	// Aggregation
	cmd.Flags().Duration("flush-interval", 5*time.Second, "aggregation flush interval")

	// Mark --server as required
	if err := cmd.MarkFlagRequired("server"); err != nil {
		panic(fmt.Sprintf("marking server flag required: %v", err))
	}

	return cmd
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	server, _ := cmd.Flags().GetString("server")
	namespaces, _ := cmd.Flags().GetStringSlice("namespace")
	allNamespaces, _ := cmd.Flags().GetBool("all-namespaces")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	tlsEnabled, _ := cmd.Flags().GetBool("tls")
	flushInterval, _ := cmd.Flags().GetDuration("flush-interval")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	// Validate mutually exclusive flags
	if len(namespaces) > 0 && allNamespaces {
		return fmt.Errorf("--namespace and --all-namespaces are mutually exclusive")
	}

	logger.Info("cpg generate configuration",
		zap.String("server", server),
		zap.Strings("namespaces", namespaces),
		zap.Bool("all-namespaces", allNamespaces),
		zap.String("output-dir", outputDir),
		zap.Bool("tls", tlsEnabled),
		zap.Duration("flush-interval", flushInterval),
		zap.Duration("timeout", timeout),
	)

	return fmt.Errorf("not yet implemented: Hubble streaming (Phase 2)")
}
