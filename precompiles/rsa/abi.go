package rsa

// ABI definition for the RSA verifier precompile
const ABI = `[
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
