package cmd

import (
	"fmt"

	"tee-mgmt-cli/registry"

	"github.com/spf13/cobra"
)

var typeCmd = &cobra.Command{
	Use:     "type",
	Aliases: []string{"types"},
	Short:   "Manage TEE type definitions (e.g. LLMProxy, Validator)",
}

var typeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered TEE types",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("=== TEE Types ===")
		for i := uint8(0); i <= 10; i++ {
			if valid, _ := client.IsValidTEEType(i); valid {
				fmt.Printf("  [%d] %s\n", i, registry.GetTEETypeName(i))
			}
		}
		return nil
	},
}

var typeAddCmd = &cobra.Command{
	Use:   "add <type_id> <name>",
	Short: "Register a new TEE type with the given numeric ID and name",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		typeId := uint8(registry.ParseUint(args[0]))
		name := args[1]
		account, _ := client.GetAccountAddress()

		registry.Log("Adding type %d: %s", typeId, name)
		txHash, err := client.AddTEEType(account, typeId, name)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		success, reason := client.WaitForTx(txHash)
		registry.PrintTxResult(success, reason, "Type added")
		return nil
	},
}

func init() {
	typeCmd.AddCommand(typeListCmd, typeAddCmd)
	rootCmd.AddCommand(typeCmd)
}
