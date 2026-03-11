package cmd

import (
	"fmt"
	"strings"

	"tee-mgmt-cli/registry"

	"github.com/spf13/cobra"
)

var roleCmd = &cobra.Command{
	Use:   "role",
	Short: "Manage access control roles (admin, operator)",
}

var roleGrantAdminCmd = &cobra.Command{
	Use:   "grant-admin <address>",
	Short: "Grant DEFAULT_ADMIN_ROLE to an address",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		account, _ := client.GetAccountAddress()
		registry.Log("Adding admin: %s", args[0])
		txHash, err := client.GrantRole(account, registry.DefaultAdminRole, args[0])
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		success, reason := client.WaitForTx(txHash)
		registry.PrintTxResult(success, reason, "Admin added")
		return nil
	},
}

var roleGrantOperatorCmd = &cobra.Command{
	Use:   "grant-operator <address>",
	Short: "Grant TEE_OPERATOR role to an address",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		account, _ := client.GetAccountAddress()
		registry.Log("Adding operator: %s", args[0])
		txHash, err := client.GrantRole(account, registry.TEEOperatorRole, args[0])
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		success, reason := client.WaitForTx(txHash)
		registry.PrintTxResult(success, reason, "Operator added")
		return nil
	},
}

var roleRevokeAdminCmd = &cobra.Command{
	Use:   "revoke-admin <address>",
	Short: "Revoke DEFAULT_ADMIN_ROLE from an address",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		account, _ := client.GetAccountAddress()
		registry.Log("Revoking admin: %s", args[0])
		txHash, err := client.RevokeRole(account, registry.DefaultAdminRole, args[0])
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		success, reason := client.WaitForTx(txHash)
		registry.PrintTxResult(success, reason, "Admin revoked")
		return nil
	},
}

var roleRevokeOperatorCmd = &cobra.Command{
	Use:   "revoke-operator <address>",
	Short: "Revoke TEE_OPERATOR role from an address",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		account, _ := client.GetAccountAddress()
		registry.Log("Revoking operator: %s", args[0])
		txHash, err := client.RevokeRole(account, registry.TEEOperatorRole, args[0])
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		success, reason := client.WaitForTx(txHash)
		registry.PrintTxResult(success, reason, "Operator revoked")
		return nil
	},
}

var roleCheckCmd = &cobra.Command{
	Use:   "check <admin|operator> <address>",
	Short: "Check whether an address has a specific role",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var roleBytes [32]byte
		var roleName string

		switch strings.ToLower(args[0]) {
		case "admin":
			roleBytes = registry.DefaultAdminRole
			roleName = "DEFAULT_ADMIN_ROLE"
		case "operator":
			roleBytes = registry.TEEOperatorRole
			roleName = "TEE_OPERATOR"
		default:
			return fmt.Errorf("unknown role: %s (use 'admin' or 'operator')", args[0])
		}

		has, _ := client.HasRole(roleBytes, args[1])
		if has {
			fmt.Printf("%s HAS %s\n", args[1], roleName)
		} else {
			fmt.Printf("%s does NOT have %s\n", args[1], roleName)
		}
		return nil
	},
}

func init() {
	roleCmd.AddCommand(roleGrantAdminCmd, roleGrantOperatorCmd, roleRevokeAdminCmd, roleRevokeOperatorCmd, roleCheckCmd)
	rootCmd.AddCommand(roleCmd)
}
