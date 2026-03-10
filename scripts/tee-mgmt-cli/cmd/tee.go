package cmd

import (
	"encoding/hex"
	"fmt"
	"os"

	"tee-mgmt-cli/registry"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spf13/cobra"
)

var teeCmd = &cobra.Command{
	Use:   "tee",
	Short: "Manage TEE instances (register, activate, deactivate, inspect)",
}

var teeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List activated TEEs for a given type",
	RunE: func(cmd *cobra.Command, args []string) error {
		teeType, _ := cmd.Flags().GetUint8("tee-type")

		fmt.Println("=== Activated TEEs in Registry ===")
		fmt.Printf("Registry: %s\n", client.RegistryAddress)
		fmt.Printf("RPC: %s\n", client.RPCURL)
		fmt.Printf("Type: %d\n\n", teeType)

		tees, err := client.GetActivatedTEEs(teeType)
		if err != nil {
			return fmt.Errorf("failed to get activated TEEs: %w", err)
		}

		fmt.Printf("Found %d activated TEE(s)\n\n", len(tees))
		for i, teeId := range tees {
			fmt.Printf("  [%d] 0x%s\n", i+1, teeId)
		}
		return nil
	},
}

var teeShowCmd = &cobra.Command{
	Use:   "show <tee_id>",
	Short: "Show detailed info for a TEE (owner, endpoint, PCR, keys, TLS cert)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		teeId, err := registry.ParseBytes32(args[0])
		if err != nil {
			return fmt.Errorf("invalid TEE ID: %w", err)
		}

		fmt.Printf("=== TEE Details: 0x%s ===\n", hex.EncodeToString(teeId[:]))

		info, err := client.GetTEE(teeId)
		if err != nil {
			return fmt.Errorf("failed to get TEE info: %w", err)
		}

		fmt.Printf("  Owner:          %s\n", info.Owner.Hex())
		fmt.Printf("  Payment Addr:   %s\n", info.PaymentAddress.Hex())
		fmt.Printf("  Endpoint:       %s\n", info.Endpoint)
		fmt.Printf("  PCR Hash:       0x%s\n", hex.EncodeToString(info.PCRHash[:]))
		fmt.Printf("  TEE Type:       %d (%s)\n", info.TEEType, registry.GetTEETypeName(info.TEEType))
		fmt.Printf("  Active:         %v\n", info.IsActive)
		fmt.Printf("  Registered:     %s UTC\n", info.RegisteredAt.UTC().Format("2006-01-02 15:04:05"))
		fmt.Printf("  Last Updated:   %s UTC\n", info.LastUpdatedAt.UTC().Format("2006-01-02 15:04:05"))

		fmt.Println("\n  --- Public Key ---")
		if len(info.PublicKey) > 0 {
			fmt.Printf("  Size: %d bytes\n", len(info.PublicKey))
			fmt.Printf("  Hex:  %s...\n", registry.Truncate(hex.EncodeToString(info.PublicKey), 64))
		} else {
			fmt.Println("  Not available")
		}

		fmt.Println("\n  --- TLS Certificate ---")
		if len(info.TLSCertificate) > 0 {
			fmt.Printf("  Size: %d bytes\n", len(info.TLSCertificate))
			fmt.Printf("  Hash: 0x%s\n", hex.EncodeToString(crypto.Keccak256(info.TLSCertificate)))
		} else {
			fmt.Println("  Not available")
		}
		return nil
	},
}

var teeRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new TEE by fetching its attestation document from the enclave",
	RunE: func(cmd *cobra.Command, args []string) error {
		enclaveHost, _ := cmd.Flags().GetString("enclave-host")
		enclavePort, _ := cmd.Flags().GetString("enclave-port")
		if enclaveHost == "" {
			return fmt.Errorf("--enclave-host is required")
		}

		account, err := client.GetAccountAddress()
		if err != nil {
			return fmt.Errorf("failed to get account: %w", err)
		}

		paymentAddr, _ := cmd.Flags().GetString("payment-address")
		if paymentAddr == "" {
			paymentAddr = account
		}
		endpoint, _ := cmd.Flags().GetString("endpoint")
		if endpoint == "" {
			endpoint = fmt.Sprintf("https://%s", enclaveHost)
		}
		teeType, _ := cmd.Flags().GetUint8("tee-type")

		fmt.Println("=== Registering TEE ===")
		fmt.Printf("  Enclave: %s:%s\n", enclaveHost, enclavePort)
		fmt.Printf("  Account: %s\n", account)
		fmt.Printf("  Payment: %s\n", paymentAddr)
		fmt.Printf("  Type:    %d\n\n", teeType)

		registry.Log("Fetching attestation document...")
		nonce := registry.GenerateNonce()
		attestDoc, err := registry.FetchAttestation(fmt.Sprintf("https://%s/enclave/attestation?nonce=%s", enclaveHost, nonce))
		if err != nil {
			return fmt.Errorf("failed to fetch attestation: %w", err)
		}
		attestBytes, _ := registry.DecodeBase64(attestDoc)
		fmt.Printf("  Attestation: %d bytes\n", len(attestBytes))

		registry.Log("Fetching signing public key...")
		signingKey, err := registry.FetchSigningPublicKey(enclaveHost)
		if err != nil {
			return fmt.Errorf("failed to fetch signing key: %w", err)
		}
		fmt.Printf("  Signing Key: %d bytes\n", len(signingKey))

		registry.Log("Fetching TLS certificate...")
		tlsCert, err := registry.FetchTLSCertificate(enclaveHost, enclavePort)
		if err != nil {
			return fmt.Errorf("failed to fetch TLS cert: %w", err)
		}
		fmt.Printf("  TLS Cert: %d bytes\n", len(tlsCert))

		expectedId := crypto.Keccak256Hash(signingKey)
		if info, err := client.GetTEE(expectedId); err == nil && info != nil {
			fmt.Printf("\nTEE already registered: 0x%s\n", hex.EncodeToString(expectedId[:]))
			return nil
		}

		registry.Log("Sending registration transaction...")
		txHash, err := client.RegisterTEE(account, attestBytes, signingKey, tlsCert, paymentAddr, endpoint, teeType)
		if err != nil {
			return fmt.Errorf("failed to register: %w", err)
		}

		fmt.Printf("  TX: %s\n", txHash)
		if client.WaitForTx(txHash) {
			fmt.Printf("\nTEE registered! ID: 0x%s\n", hex.EncodeToString(expectedId[:]))
		} else {
			fmt.Println("\nRegistration failed")
			os.Exit(1)
		}
		return nil
	},
}

var teeDeactivateCmd = &cobra.Command{
	Use:   "deactivate <tee_id>",
	Short: "Deactivate a TEE (requires admin or operator role)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		teeId, err := registry.ParseBytes32(args[0])
		if err != nil {
			return fmt.Errorf("invalid TEE ID: %w", err)
		}
		account, _ := client.GetAccountAddress()

		registry.Log("Deactivating TEE: 0x%s", hex.EncodeToString(teeId[:]))
		txHash, err := client.DeactivateTEE(account, teeId)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		registry.PrintTxResult(client.WaitForTx(txHash), "TEE deactivated")
		return nil
	},
}

var teeActivateCmd = &cobra.Command{
	Use:   "activate <tee_id>",
	Short: "Re-activate a previously deactivated TEE",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		teeId, err := registry.ParseBytes32(args[0])
		if err != nil {
			return fmt.Errorf("invalid TEE ID: %w", err)
		}
		account, _ := client.GetAccountAddress()

		registry.Log("Activating TEE: 0x%s", hex.EncodeToString(teeId[:]))
		txHash, err := client.ActivateTEE(account, teeId)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		registry.PrintTxResult(client.WaitForTx(txHash), "TEE activated")
		return nil
	},
}

var teeRemoveCmd = &cobra.Command{
	Use:   "remove <tee_id>",
	Short: "Permanently remove a TEE from the registry (owner or admin)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		teeId, err := registry.ParseBytes32(args[0])
		if err != nil {
			return fmt.Errorf("invalid TEE ID: %w", err)
		}
		account, _ := client.GetAccountAddress()

		registry.Log("Removing TEE: 0x%s", hex.EncodeToString(teeId[:]))
		txHash, err := client.RemoveTEE(account, teeId)
		if err != nil {
			return fmt.Errorf("failed: %w", err)
		}
		fmt.Printf("TX: %s\n", txHash)
		registry.PrintTxResult(client.WaitForTx(txHash), "TEE removed")
		return nil
	},
}

func init() {
	teeListCmd.Flags().Uint8("tee-type", 0, "TEE type ID to list")

	teeRegisterCmd.Flags().String("enclave-host", "", "Enclave hostname or IP (required)")
	teeRegisterCmd.Flags().String("enclave-port", "443", "Enclave TLS port for certificate fetch")
	teeRegisterCmd.Flags().String("payment-address", "", "Payment address for the TEE (defaults to sender)")
	teeRegisterCmd.Flags().String("endpoint", "", "Public endpoint URL for the TEE (defaults to https://<enclave-host>)")
	teeRegisterCmd.Flags().Uint8("tee-type", 0, "TEE type ID (e.g. 0=LLMProxy, 1=Validator)")
	teeRegisterCmd.MarkFlagRequired("enclave-host")

	teeCmd.AddCommand(teeListCmd, teeShowCmd, teeRegisterCmd, teeDeactivateCmd, teeActivateCmd, teeRemoveCmd)
	rootCmd.AddCommand(teeCmd)
}
