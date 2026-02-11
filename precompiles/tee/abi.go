package tee

// ABI definition for the TEE verifier precompile (combines RSA and attestation)
const ABI = `[
	{
		"type": "function",
		"name": "verifyAttestation",
		"inputs": [
			{
				"name": "attestationDocument",
				"type": "bytes",
				"internalType": "bytes"
			},
			{
				"name": "signingPublicKey",
				"type": "bytes",
				"internalType": "bytes"
			},
			{
				"name": "tlsCertificate",
				"type": "bytes",
				"internalType": "bytes"
			},
			{
				"name": "rootCertificate",
				"type": "bytes",
				"internalType": "bytes"
			}
		],
		"outputs": [
			{
				"name": "valid",
				"type": "bool",
				"internalType": "bool"
			},
			{
				"name": "pcrHash",
				"type": "bytes32",
				"internalType": "bytes32"
			}
		],
		"stateMutability": "view"
	},
	{
		"type": "function",
		"name": "verifyRSAPSS",
		"inputs": [
			{
				"name": "publicKeyDER",
				"type": "bytes",
				"internalType": "bytes"
			},
			{
				"name": "messageHash",
				"type": "bytes32",
				"internalType": "bytes32"
			},
			{
				"name": "signature",
				"type": "bytes",
				"internalType": "bytes"
			}
		],
		"outputs": [
			{
				"name": "valid",
				"type": "bool",
				"internalType": "bool"
			}
		],
		"stateMutability": "view"
	}
]`
