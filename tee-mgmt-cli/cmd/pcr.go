package cmd

import (
	"encoding/hex"
	"fmt"

	"tee-mgmt-cli/registry"

	"github.com/spf13/cobra"
)

var pcrCmd = &cobra.Command{
	Use:   "pcr",
	Short: "Manage PCR (Platform Configuration Register) measurement approvals",
}

var pcrListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all currently approved PCR hashes",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("=== Approved PCRs ===")
		pcrs, err := client.GetApprovedPCRs()
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("Found %d approved PCR(s)\n\n", len(pcrs))
		for i, k := range pcrs {
			fmt.Printf("  [%d] 0x%s (type: %d)\n", i+1, hex.EncodeToString(k.PCRHash[:]), k.TEEType)
		}
		return nil
	},
}

var pcrApproveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve a set of PCR measurements (PCR0, PCR1, PCR2) for a specific TEE type",
	RunE: func(cmd *cobra.Command, args []string) error {
		measurementsFile, _ := cmd.Flags().GetString("measurements-file")
		pcr0Hex, _ := cmd.Flags().GetString("pcr0")
		pcr1Hex, _ := cmd.Flags().GetString("pcr1")
		pcr2Hex, _ := cmd.Flags().GetString("pcr2")
		version, _ := cmd.Flags().GetString("version")
		teeType, _ := cmd.Flags().GetUint8("tee-type")

		pcr0, pcr1, pcr2 := registry.LoadPCRsFromArgs(measurementsFile, pcr0Hex, pcr1Hex, pcr2Hex)

		pcrHash, _ := client.ComputePCRHash(pcr0, pcr1, pcr2)

		fmt.Println("=== Approving PCR ===")
		fmt.Printf("  PCR Hash: 0x%s\n", hex.EncodeToString(pcrHash[:]))
		fmt.Printf("  Version:  %s\n", version)
		fmt.Printf("  TEE Type: %d\n\n", teeType)

		account, _ := client.GetAccountAddress()
		txHash, err := client.ApprovePCR(account, pcr0, pcr1, pcr2, version, teeType)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		success, reason := client.WaitForTx(txHash)
		registry.PrintTxResult(success, reason, "PCR approved")
		return nil
	},
}

var pcrRevokeCmd = &cobra.Command{
	Use:   "revoke <pcr_hash>",
	Short: "Revoke a previously approved PCR hash and disable all TEEs using it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pcrHash, err := registry.ParseBytes32(args[0])
		if err != nil {
			return fmt.Errorf("invalid pcrHash: %w", err)
		}
		teeType, _ := cmd.Flags().GetUint8("tee-type")

		account, _ := client.GetAccountAddress()

		registry.Log("Revoking PCR: 0x%s (type: %d)", hex.EncodeToString(pcrHash[:]), teeType)
		txHash, err := client.RevokePCR(account, pcrHash, teeType)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		success, reason := client.WaitForTx(txHash)
		registry.PrintTxResult(success, reason, "PCR revoked")
		return nil
	},
}

var pcrCheckCmd = &cobra.Command{
	Use:   "check <pcr_hash>",
	Short: "Check whether a PCR hash is currently approved for a TEE type",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pcrHash, err := registry.ParseBytes32(args[0])
		if err != nil {
			return fmt.Errorf("invalid pcrHash: %w", err)
		}
		teeType, _ := cmd.Flags().GetUint8("tee-type")

		approved, _ := client.IsPCRApproved(teeType, pcrHash)
		if approved {
			fmt.Printf("PCR 0x%s is APPROVED for type %d\n", hex.EncodeToString(pcrHash[:]), teeType)
		} else {
			fmt.Printf("PCR 0x%s is NOT approved for type %d\n", hex.EncodeToString(pcrHash[:]), teeType)
		}
		return nil
	},
}

var pcrComputeCmd = &cobra.Command{
	Use:   "compute",
	Short: "Compute the keccak256 hash from PCR0/PCR1/PCR2 measurements without submitting",
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
	cmd.Flags().StringP("measurements-file", "m", "", "Path to measurements JSON file (alternative to --pcr0/1/2)")
	cmd.Flags().String("pcr0", "", "PCR0 measurement (hex)")
	cmd.Flags().String("pcr1", "", "PCR1 measurement (hex)")
	cmd.Flags().String("pcr2", "", "PCR2 measurement (hex)")
}

func init() {
	addPCRFlags(pcrApproveCmd)
	pcrApproveCmd.Flags().StringP("version", "v", "v1.0.0", "Version label for this PCR set")
	pcrApproveCmd.Flags().Uint8("tee-type", 0, "TEE type ID this PCR is valid for")

	pcrRevokeCmd.Flags().Uint8("tee-type", 0, "TEE type ID the PCR is approved for")

	pcrCheckCmd.Flags().Uint8("tee-type", 0, "TEE type ID to check against")

	addPCRFlags(pcrComputeCmd)

	pcrCmd.AddCommand(pcrListCmd, pcrApproveCmd, pcrRevokeCmd, pcrCheckCmd, pcrComputeCmd)
	rootCmd.AddCommand(pcrCmd)
}
