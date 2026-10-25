package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ghostkey",
	Short: "GhostKey - Kubernetes eBPF HTTPS Interceptor",
	Long: `GhostKey transparently intercepts HTTPS traffic in Kubernetes pods,
rewrites hashed headers to original values, and enables secure
API key management without exposing secrets in plain text.`,
}

func init() {
	rootCmd.AddCommand(controllerCmd)
	rootCmd.AddCommand(webhookCmd)
	rootCmd.AddCommand(versionCmd)

	// Wire `ghostkey --version` to the same multi-line output as `ghostkey version`
	// instead of Cobra's default one-liner, so the two surfaces stay in sync.
	rootCmd.Version = version
	rootCmd.SetVersionTemplate(versionString())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
