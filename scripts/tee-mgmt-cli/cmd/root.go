package cmd

import (
	"fmt"
	"os"
	"strings"

	"tee-mgmt-cli/registry"

	"github.com/spf13/cobra"
)

var client *registry.Client

var rootCmd = &cobra.Command{
	Use:   "tee-cli",
	Short: "CLI for managing the OpenGradient TEE Registry contract",
	Long: `CLI for managing the OpenGradient TEE Registry contract.

Supports TEE registration/lifecycle, PCR approval, role management,
TEE type configuration, and AWS root certificate setup.

Global connection flags can also be set via environment variables
(RPC_URL, TEE_REGISTRY_ADDRESS, TEE_PRIVATE_KEY) or a .env file.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		rpcURL, _ := cmd.Flags().GetString("rpc-url")
		registryAddr, _ := cmd.Flags().GetString("registry")
		privateKey, _ := cmd.Flags().GetString("private-key")
		client = registry.NewClient(rpcURL, registryAddr, privateKey)
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	loadEnvFile(".env")

	rootCmd.PersistentFlags().String("rpc-url", getEnvOrDefault("RPC_URL", "https://ogevmdevnet.opengradient.ai"), "OpenGradient network RPC endpoint URL")
	rootCmd.PersistentFlags().String("registry", getEnvOrDefault("TEE_REGISTRY_ADDRESS", "0x4e72238852f3c918f4E4e57AeC9280dDB0c80248"), "TEE Registry contract address (hex)")
	rootCmd.PersistentFlags().String("private-key", os.Getenv("TEE_PRIVATE_KEY"), "Private key for signing transactions (hex, omit 0x prefix)")
}

func loadEnvFile(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if os.Getenv(key) == "" && value != "" {
			os.Setenv(key, value)
		}
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
