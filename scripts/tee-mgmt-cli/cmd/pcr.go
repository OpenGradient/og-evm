package cmd

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"tee-mgmt-cli/registry"

	"github.com/spf13/cobra"
)

var pcrCmd = &cobra.Command{
	Use:   "pcr",
	Short: "PCR management commands",
}

var pcrListCmd = &cobra.Command{
	Use:   "list",
	Short: "List approved PCRs",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("=== Active PCRs ===")
		pcrs, err := client.GetActivePCRs()
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("Found %d active PCR(s)\n\n", len(pcrs))
		for i, h := range pcrs {
			fmt.Printf("  [%d] 0x%s\n", i+1, h)
		}
		return nil
	},
}

var pcrApproveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve PCR measurements",
	RunE: func(cmd *cobra.Command, args []string) error {
		measurementsFile, _ := cmd.Flags().GetString("measurements-file")
		pcr0Hex, _ := cmd.Flags().GetString("pcr0")
		pcr1Hex, _ := cmd.Flags().GetString("pcr1")
		pcr2Hex, _ := cmd.Flags().GetString("pcr2")
		version, _ := cmd.Flags().GetString("version")
		gracePeriodStr, _ := cmd.Flags().GetString("grace-period")
		prevPCRStr, _ := cmd.Flags().GetString("previous-pcr")

		pcr0, pcr1, pcr2 := registry.LoadPCRsFromArgs(measurementsFile, pcr0Hex, pcr1Hex, pcr2Hex)

		gracePeriod := new(big.Int)
		gracePeriod.SetString(gracePeriodStr, 10)

		var prevPCR [32]byte
		if prevPCRStr != "" {
			var err error
			prevPCR, err = registry.ParseBytes32(prevPCRStr)
			if err != nil {
				return fmt.Errorf("invalid --previous-pcr: %w", err)
			}
		}

		pcrHash, _ := client.ComputePCRHash(pcr0, pcr1, pcr2)

		fmt.Println("=== Approving PCR ===")
		fmt.Printf("  PCR Hash: 0x%s\n", hex.EncodeToString(pcrHash[:]))
		fmt.Printf("  Version:  %s\n\n", version)

		account, _ := client.GetAccountAddress()
		txHash, err := client.ApprovePCR(account, pcr0, pcr1, pcr2, version, prevPCR, gracePeriod)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		registry.PrintTxResult(client.WaitForTx(txHash), "PCR approved")
		return nil
	},
}

var pcrRevokeCmd = &cobra.Command{
	Use:   "revoke <pcr_hash>",
	Short: "Revoke a PCR",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pcrHash, err := registry.ParseBytes32(args[0])
		if err != nil {
			return fmt.Errorf("invalid pcrHash: %w", err)
		}
		account, _ := client.GetAccountAddress()

		registry.Log("Revoking PCR: 0x%s", hex.EncodeToString(pcrHash[:]))
		txHash, err := client.RevokePCR(account, pcrHash)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		registry.PrintTxResult(client.WaitForTx(txHash), "PCR revoked")
		return nil
	},
}

var pcrCheckCmd = &cobra.Command{
	Use:   "check <pcr_hash>",
	Short: "Check if a PCR is approved",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pcrHash, err := registry.ParseBytes32(args[0])
		if err != nil {
			return fmt.Errorf("invalid pcrHash: %w", err)
		}

		approved, _ := client.IsPCRApproved(pcrHash)
		if approved {
			fmt.Printf("PCR 0x%s is APPROVED\n", hex.EncodeToString(pcrHash[:]))
		} else {
			fmt.Printf("PCR 0x%s is NOT approved\n", hex.EncodeToString(pcrHash[:]))
		}
		return nil
	},
}

var pcrComputeCmd = &cobra.Command{
	Use:   "compute",
	Short: "Compute hash from measurements",
	RunE: func(cmd *cobra.Command, args []string) error {
		measurementsFile, _ := cmd.Flags().GetString("measurements-file")
		pcr0Hex, _ := cmd.Flags().GetString("pcr0")
		pcr1Hex, _ := cmd.Flags().GetString("pcr1")
		pcr2Hex, _ := cmd.Flags().GetString("pcr2")

		pcr0, pcr1, pcr2 := registry.LoadPCRsFromArgs(measurementsFile, pcr0Hex, pcr1Hex, pcr2Hex)
		pcrHash, err := client.ComputePCRHash(pcr0, pcr1, pcr2)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("PCR Hash: 0x%s\n", hex.EncodeToString(pcrHash[:]))
		return nil
	},
}

func addPCRFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("measurements-file", "m", "", "Path to measurements JSON file")
	cmd.Flags().String("pcr0", "", "PCR0 hex value")
	cmd.Flags().String("pcr1", "", "PCR1 hex value")
	cmd.Flags().String("pcr2", "", "PCR2 hex value")
}

func init() {
	addPCRFlags(pcrApproveCmd)
	pcrApproveCmd.Flags().StringP("version", "v", "v1.0.0", "PCR version string")
	pcrApproveCmd.Flags().String("grace-period", "0", "Grace period in seconds")
	pcrApproveCmd.Flags().String("previous-pcr", "", "Previous PCR hash (bytes32)")

	addPCRFlags(pcrComputeCmd)

	pcrCmd.AddCommand(pcrListCmd, pcrApproveCmd, pcrRevokeCmd, pcrCheckCmd, pcrComputeCmd)
	rootCmd.AddCommand(pcrCmd)
}
