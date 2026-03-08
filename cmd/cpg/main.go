package main

import (
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync() //nolint:errcheck

	rootCmd := &cobra.Command{
		Use:     "cpg",
		Short:   "Cilium Policy Generator",
		Long:    "Automatically generate CiliumNetworkPolicies from observed Hubble flow denials.",
		Version: version,
	}

	if err := rootCmd.Execute(); err != nil {
		logger.Error("command failed", zap.Error(err))
		os.Exit(1)
	}
}
