package cmd

import (
	"fmt"

	"tee-mgmt-cli/registry"

	"github.com/spf13/cobra"
)

var typeCmd = &cobra.Command{
	Use:     "type",
	Aliases: []string{"types"},
	Short:   "TEE type management commands",
}

var typeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List TEE types",
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
	Short: "Add a TEE type",
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
		registry.PrintTxResult(client.WaitForTx(txHash), "Type added")
		return nil
	},
}

var typeDeactivateCmd = &cobra.Command{
	Use:   "deactivate <type_id>",
	Short: "Deactivate a TEE type",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		typeId := uint8(registry.ParseUint(args[0]))
		account, _ := client.GetAccountAddress()

		registry.Log("Deactivating type %d", typeId)
		txHash, err := client.DeactivateTEEType(account, typeId)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		registry.PrintTxResult(client.WaitForTx(txHash), "Type deactivated")
		return nil
	},
}

func init() {
	typeCmd.AddCommand(typeListCmd, typeAddCmd, typeDeactivateCmd)
	rootCmd.AddCommand(typeCmd)
}
