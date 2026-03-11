package cmd

import (
	"fmt"
	"math/big"

	"tee-mgmt-cli/registry"

	"github.com/spf13/cobra"
)

var heartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "Manage heartbeat configuration",
}

var heartbeatSetMaxAgeCmd = &cobra.Command{
	Use:   "set-max-age <seconds>",
	Short: "Set the max allowed age for heartbeat timestamps (admin only)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		maxAge, ok := new(big.Int).SetString(args[0], 10)
		if !ok || maxAge.Sign() < 0 {
			return fmt.Errorf("invalid max age: %s (must be a non-negative integer)", args[0])
		}
		if maxAge.BitLen() > 256 {
			return fmt.Errorf("invalid max age: %s (must fit into a 256-bit unsigned integer)", args[0])
		}

		account, err := client.GetAccountAddress()
		if err != nil {
			return fmt.Errorf("failed to get account: %w", err)
		}

		registry.Log("Setting heartbeat max age to %s seconds", maxAge.String())
		txHash, err := client.SetHeartbeatMaxAge(account, maxAge)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		registry.PrintTxResult(client.WaitForTx(txHash), fmt.Sprintf("Heartbeat max age set to %s seconds", maxAge.String()))
		return nil
	},
}

var heartbeatGetMaxAgeCmd = &cobra.Command{
	Use:   "get-max-age",
	Short: "Get the current heartbeat max age setting",
	RunE: func(cmd *cobra.Command, args []string) error {
		maxAge, err := client.GetHeartbeatMaxAge()
		if err != nil {
			return fmt.Errorf("failed to get heartbeat max age: %w", err)
		}
		fmt.Printf("Heartbeat max age: %s seconds\n", maxAge.String())
		return nil
	},
}

func init() {
	heartbeatCmd.AddCommand(heartbeatSetMaxAgeCmd, heartbeatGetMaxAgeCmd)
	rootCmd.AddCommand(heartbeatCmd)
}
