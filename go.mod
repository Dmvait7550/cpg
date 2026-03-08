module github.com/gule/cpg

go 1.25.1

require (
	github.com/spf13/cobra v1.10.2
	go.uber.org/zap v1.27.1
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
)

replace sigs.k8s.io/controller-tools => github.com/cilium/controller-tools v0.16.5-1

replace github.com/osrg/gobgp/v3 => github.com/cilium/gobgp/v3 v3.0.0-20260130142103-27e5da2a39e6
