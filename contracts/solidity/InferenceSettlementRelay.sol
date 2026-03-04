// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/utils/cryptography/MerkleProof.sol";
import "./TEEInferenceVerifier.sol";

/**
 * @title InferenceSettlementRelay
 * @notice Emits settlement-related events after optional signature validation.
 * @dev This relay is intended as an integration boundary between off-chain facilitators
 * and on-chain consumers. Signature validation is delegated to `TEEInferenceVerifier`.
 */
contract InferenceSettlementRelay is AccessControl {
    /// @notice Role identifier for accounts authorized to perform relay settlement actions.
    bytes32 public constant SETTLEMENT_RELAY_ROLE = keccak256("SETTLEMENT_RELAY_ROLE");

    /// @notice TEEInferenceVerifier used for cryptographic and timestamp verification.
    TEEInferenceVerifier public immutable SETTLEMENT_CONTRACT;

    /**
     * @notice Emitted when a batch settlement root is relayed.
     * @param merkleRoot Merkle root representing the settled batch.
     * @param batchSize Number of individual settlements included in the batch.
     * @param walrusBlobId Off-chain blob identifier containing batch metadata.
     */
    event BatchSettlement(
        bytes32 indexed merkleRoot,
        uint256 batchSize,
        bytes walrusBlobId
    );

    /**
     * @notice Emitted when a single settlement is relayed after signature verification.
     * @param teeId Unique identifier of the TEE instance that signed the settlement.
     * @param ethAddress Ethereum address associated with the settled identity/account.
     * @param inputHash Hash of the settlement input payload.
     * @param outputHash Hash of the settlement output payload.
     * @param timestamp Unix timestamp associated with the attested settlement.
     * @param walrusBlobId Off-chain blob identifier for the related payload.
     * @param signature Signature used to validate settlement authenticity.
     */
    event IndividualSettlement(
        bytes32 indexed teeId,
        address indexed ethAddress,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes walrusBlobId,
        bytes signature
    );

    /**
     * @notice Initializes the relay with a TEEInferenceVerifier contract.
     * @dev Reverts if `_settlement_contract` is the zero address.
     * @param _settlement_contract Address of the deployed TEEInferenceVerifier.
     */
    constructor(address _settlement_contract) {
        require(_settlement_contract != address(0), "Invalid settlement contract");

        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(SETTLEMENT_RELAY_ROLE, msg.sender);

        SETTLEMENT_CONTRACT = TEEInferenceVerifier(_settlement_contract);
    }

    // --- WRITE FUNCTIONS (Only Relay Role) ---

    /**
     * @notice Relays a batch settlement by emitting the batch event.
     * @dev Access restricted to accounts with `SETTLEMENT_RELAY_ROLE`.
     * @param _merkleRoot Merkle root representing the submitted batch.
     * @param _batchSize Number of leaves represented by `_merkleRoot`.
     * @param _walrusBlobId Off-chain blob identifier containing batch details.
     */
    function batchSettle(
        bytes32 _merkleRoot,
        uint256 _batchSize,
        bytes calldata _walrusBlobId
    ) external onlyRole(SETTLEMENT_RELAY_ROLE) {
        emit BatchSettlement(_merkleRoot, _batchSize, _walrusBlobId);
    }

    /**
     * @notice Relays an individual settlement after validating its signature.
     * @dev Access restricted to accounts with `SETTLEMENT_RELAY_ROLE`.
     * Reverts if the settlement signature check fails in `SETTLEMENT_CONTRACT`.
     * @param _teeId Unique identifier of the TEE instance that signed the settlement.
     * @param _inputHash Hash of the settlement input payload.
     * @param _outputHash Hash of the settlement output payload.
     * @param _timestamp Unix timestamp embedded in the signed payload.
     * @param _ethAddress Ethereum address associated with this settlement.
     * @param _walrusBlobId Off-chain blob identifier containing settlement details.
     * @param _signature Signature for settlement verification.
     */
    function settleIndividual(
        bytes32 _teeId,
        bytes32 _inputHash,
        bytes32 _outputHash,
        uint256 _timestamp,
        address _ethAddress,
        bytes calldata _walrusBlobId,
        bytes calldata _signature
    ) external onlyRole(SETTLEMENT_RELAY_ROLE){
        require(SETTLEMENT_CONTRACT.verifySignature(_teeId, _inputHash, _outputHash, _timestamp, _signature), "Invalid signature");
        emit IndividualSettlement(
            _teeId,
            _ethAddress,
            _inputHash,
            _outputHash,
            _timestamp,
            _walrusBlobId,
            _signature
        );
    }

    // --- READ / VERIFY FUNCTIONS (Public) ---

    /**
     * @notice Verifies a Merkle inclusion proof against a root and leaf.
     * @param _proof Ordered proof path from leaf to root.
     * @param _root Expected Merkle root.
     * @param _leaf Leaf value to prove.
     * @return True if the proof is valid for the given root and leaf.
     */
    function verifyProof(
        bytes32[] calldata _proof,
        bytes32 _root,
        bytes32 _leaf
    ) external pure returns (bool) {
        return MerkleProof.verify(_proof, _root, _leaf);
    }
}
