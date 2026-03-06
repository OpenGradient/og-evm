package cmd

import (
	"bytes"
	"encoding/pem"
	"fmt"
	"os"

	"tee-mgmt-cli/registry"

	"github.com/spf13/cobra"
)

var certCmd = &cobra.Command{
	Use:   "cert",
	Short: "Manage root certificates used for TEE attestation verification",
}

var certSetAWSCmd = &cobra.Command{
	Use:   "set-aws <cert_file>",
	Short: "Set the AWS Nitro Enclaves root certificate (PEM or DER)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		certData, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}

		if bytes.Contains(certData, []byte("-----BEGIN")) {
			if block, _ := pem.Decode(certData); block != nil {
				certData = block.Bytes
			}
		}

		account, _ := client.GetAccountAddress()
		registry.Log("Setting AWS cert (%d bytes)", len(certData))

		txHash, err := client.SetAWSRootCertificate(account, certData)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		registry.PrintTxResult(client.WaitForTx(txHash), "AWS cert set")
		return nil
	},
}

func init() {
	certCmd.AddCommand(certSetAWSCmd)
	rootCmd.AddCommand(certCmd)
}
