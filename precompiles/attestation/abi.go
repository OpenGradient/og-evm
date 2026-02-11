package attestation

// ABI definition for the attestation verifier precompile
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
	}
]`
