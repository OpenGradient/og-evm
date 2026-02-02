package tee

// ABI defines the TEE Registry precompile interface
const ABI = `[
	{
		"name": "addAdmin",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{"name": "newAdmin", "type": "address"}],
		"outputs": []
	},
	{
		"name": "removeAdmin",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{"name": "admin", "type": "address"}],
		"outputs": []
	},
	{
		"name": "isAdmin",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "account", "type": "address"}],
		"outputs": [{"name": "", "type": "bool"}]
	},
	{
		"name": "getAdmins",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [{"name": "", "type": "address[]"}]
	},
	{
		"name": "addTEEType",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "typeId", "type": "uint8"},
			{"name": "name", "type": "string"}
		],
		"outputs": []
	},
	{
		"name": "deactivateTEEType",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{"name": "typeId", "type": "uint8"}],
		"outputs": []
	},
	{
		"name": "isValidTEEType",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "typeId", "type": "uint8"}],
		"outputs": [{"name": "", "type": "bool"}]
	},
	{
		"name": "getTEETypes",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [{
			"name": "",
			"type": "tuple[]",
			"components": [
				{"name": "typeId", "type": "uint8"},
				{"name": "name", "type": "string"},
				{"name": "active", "type": "bool"},
				{"name": "addedAt", "type": "uint256"}
			]
		}]
	},
	{
		"name": "approvePCR",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{
				"name": "pcrs",
				"type": "tuple",
				"components": [
					{"name": "pcr0", "type": "bytes"},
					{"name": "pcr1", "type": "bytes"},
					{"name": "pcr2", "type": "bytes"}
				]
			},
			{"name": "version", "type": "string"},
			{"name": "previousPcrHash", "type": "bytes32"},
			{"name": "gracePeriod", "type": "uint256"}
		],
		"outputs": []
	},
	{
		"name": "revokePCR",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{"name": "pcrHash", "type": "bytes32"}],
		"outputs": []
	},
	{
		"name": "isPCRApproved",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{
			"name": "pcrs",
			"type": "tuple",
			"components": [
				{"name": "pcr0", "type": "bytes"},
				{"name": "pcr1", "type": "bytes"},
				{"name": "pcr2", "type": "bytes"}
			]
		}],
		"outputs": [{"name": "", "type": "bool"}]
	},
	{
		"name": "getActivePCRs",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [{"name": "", "type": "bytes32[]"}]
	},
	{
		"name": "getPCRDetails",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "pcrHash", "type": "bytes32"}],
		"outputs": [{
			"name": "",
			"type": "tuple",
			"components": [
				{"name": "pcrHash", "type": "bytes32"},
				{"name": "active", "type": "bool"},
				{"name": "approvedAt", "type": "uint256"},
				{"name": "expiresAt", "type": "uint256"},
				{"name": "version", "type": "string"}
			]
		}]
	},
	{
		"name": "computePCRHash",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [{
			"name": "pcrs",
			"type": "tuple",
			"components": [
				{"name": "pcr0", "type": "bytes"},
				{"name": "pcr1", "type": "bytes"},
				{"name": "pcr2", "type": "bytes"}
			]
		}],
		"outputs": [{"name": "", "type": "bytes32"}]
	},
	{
		"name": "setAWSRootCertificate",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [{"name": "certificate", "type": "bytes"}],
		"outputs": []
	},
	{
		"name": "getAWSRootCertificateHash",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [{"name": "", "type": "bytes32"}]
	},
	{
		"name": "registerTEEWithAttestation",
		"type": "function",
		"stateMutability": "nonpayable",
		"inputs": [
			{"name": "attestationDocument", "type": "bytes"},
			{"name": "paymentAddress", "type": "address"},
			{"name": "endpoint", "type": "string"},
			{"name": "teeType", "type": "uint8"}
		],
		"outputs": [{"name": "teeId", "type": "bytes32"}]
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
		"inputs": [{
			"name": "request",
			"type": "tuple",
			"components": [
				{"name": "teeId", "type": "bytes32"},
				{"name": "requestHash", "type": "bytes32"},
				{"name": "responseHash", "type": "bytes32"},
				{"name": "timestamp", "type": "uint256"},
				{"name": "signature", "type": "bytes"}
			]
		}],
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
			"name": "",
			"type": "tuple",
			"components": [
				{"name": "teeId", "type": "bytes32"},
				{"name": "owner", "type": "address"},
				{"name": "paymentAddress", "type": "address"},
				{"name": "endpoint", "type": "string"},
				{"name": "publicKey", "type": "bytes"},
				{"name": "pcrHash", "type": "bytes32"},
				{"name": "teeType", "type": "uint8"},
				{"name": "active", "type": "bool"},
				{"name": "registeredAt", "type": "uint256"},
				{"name": "lastUpdatedAt", "type": "uint256"}
			]
		}]
	},
	{
		"name": "getActiveTEEs",
		"type": "function",
		"stateMutability": "view",
		"inputs": [],
		"outputs": [{"name": "", "type": "bytes32[]"}]
	},
	{
		"name": "getTEEsByType",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "teeType", "type": "uint8"}],
		"outputs": [{"name": "", "type": "bytes32[]"}]
	},
	{
		"name": "getTEEsByOwner",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "owner", "type": "address"}],
		"outputs": [{"name": "", "type": "bytes32[]"}]
	},
	{
		"name": "getPublicKey",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "teeId", "type": "bytes32"}],
		"outputs": [{"name": "", "type": "bytes"}]
	},
	{
		"name": "isActive",
		"type": "function",
		"stateMutability": "view",
		"inputs": [{"name": "teeId", "type": "bytes32"}],
		"outputs": [{"name": "", "type": "bool"}]
	},
	{
		"name": "computeTEEId",
		"type": "function",
		"stateMutability": "pure",
		"inputs": [{"name": "publicKey", "type": "bytes"}],
		"outputs": [{"name": "", "type": "bytes32"}]
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
		"outputs": [{"name": "", "type": "bytes32"}]
	}
]`
