package tee

// ABI is the JSON ABI for the TEE Registry precompile
const ABI = `[
	{
		"name": "registerTEE",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "publicKey", "type": "bytes"},
			{"name": "pcrs", "type": "tuple", "components": [
				{"name": "pcr0", "type": "bytes32"},
				{"name": "pcr1", "type": "bytes32"},
				{"name": "pcr2", "type": "bytes32"}
			]}
		],
		"outputs": [{"name": "teeId", "type": "bytes32"}]
	},
	{
		"name": "registerTEEWithAttestation",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "attestationDocument", "type": "bytes"},
			{"name": "expectedPcrs", "type": "tuple", "components": [
				{"name": "pcr0", "type": "bytes32"},
				{"name": "pcr1", "type": "bytes32"},
				{"name": "pcr2", "type": "bytes32"}
			]}
		],
		"outputs": [{"name": "teeId", "type": "bytes32"}]
	},
	{
		"name": "verifyAttestation",
		"type": "function",
		"stateMutability": "view",
		"inputs": [
			{"name": "attestationDocument", "type": "bytes"},
			{"name": "expectedPcrs", "type": "tuple", "components": [
				{"name": "pcr0", "type": "bytes32"},
				{"name": "pcr1", "type": "bytes32"},
				{"name": "pcr2", "type": "bytes32"}
			]}
		],
		"outputs": [
			{"name": "valid", "type": "bool"},
			{"name": "publicKey", "type": "bytes"}
		]
	},
	{
		"name": "deactivateTEE",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{"name": "teeId", "type": "bytes32"}],
		"outputs": []
	},
	{
		"name": "activateTEE",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{"name": "teeId", "type": "bytes32"}],
		"outputs": []
	},
	{
		"name": "verifySignature",
		"type": "function",
		"stateMutability": "view",
		"inputs": [
			{"name": "teeId", "type": "bytes32"},
			{"name": "inputHash", "type": "bytes32"},
			{"name": "outputHash", "type": "bytes32"},
			{"name": "timestamp", "type": "uint256"},
			{"name": "signature", "type": "bytes"}
		],
		"outputs": [{"name": "valid", "type": "bool"}]
	},
	{
		"name": "verifySettlement",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "teeId", "type": "bytes32"},
			{"name": "inputHash", "type": "bytes32"},
			{"name": "outputHash", "type": "bytes32"},
			{"name": "timestamp", "type": "uint256"},
			{"name": "signature", "type": "bytes"}
		],
		"outputs": [{"name": "valid", "type": "bool"}]
	},
	{
		"name": "getTEE",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "teeId", "type": "bytes32"}],
		"outputs": [{
			"name": "info",
			"type": "tuple",
			"components": [
				{"name": "teeId", "type": "bytes32"},
				{"name": "owner", "type": "address"},
				{"name": "publicKey", "type": "bytes"},
				{"name": "pcrs", "type": "tuple", "components": [
					{"name": "pcr0", "type": "bytes32"},
					{"name": "pcr1", "type": "bytes32"},
					{"name": "pcr2", "type": "bytes32"}
				]},
				{"name": "active", "type": "bool"},
				{"name": "registeredAt", "type": "uint256"},
				{"name": "lastUpdatedAt", "type": "uint256"}
			]
		}]
	},
	{
		"name": "isActive",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "teeId", "type": "bytes32"}],
		"outputs": [{"name": "active", "type": "bool"}]
	},
	{
		"name": "getPublicKey",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "teeId", "type": "bytes32"}],
		"outputs": [{"name": "publicKey", "type": "bytes"}]
	},
	{
		"name": "computeTEEId",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [{"name": "publicKey", "type": "bytes"}],
		"outputs": [{"name": "teeId", "type": "bytes32"}]
	},
	{
		"name": "computeMessageHash",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [
			{"name": "inputHash", "type": "bytes32"},
			{"name": "outputHash", "type": "bytes32"},
			{"name": "timestamp", "type": "uint256"}
		],
		"outputs": [{"name": "messageHash", "type": "bytes32"}]
	}
]`
