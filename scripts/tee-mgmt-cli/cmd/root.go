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
	Short: "TEE Registry Management CLI",
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

	rootCmd.PersistentFlags().String("rpc-url", getEnvOrDefault("RPC_URL", "http://13.59.43.94:8545"), "RPC endpoint URL")
	rootCmd.PersistentFlags().String("registry", getEnvOrDefault("TEE_REGISTRY_ADDRESS", "0x3d641a2791533b4a0000345ea8d509d01e1ec301"), "TEE Registry contract address")
	rootCmd.PersistentFlags().String("private-key", os.Getenv("TEE_PRIVATE_KEY"), "Private key for signing transactions")
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
