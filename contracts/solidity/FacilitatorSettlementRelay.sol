// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/utils/cryptography/MerkleProof.sol";

/**
 * @title ISettlementContract
 * @notice Interface for the settlement verification contract consumed by the relay.
 * @dev The relay delegates signature and settlement validation to this external contract.
 */
interface ISettlementContract {
    /**
     * @notice Verifies and records a settlement payload in the settlement contract.
     * @param teeId Unique identifier of the TEE instance that produced the attestation.
     * @param inputHash Hash of the settlement input payload.
     * @param outputHash Hash of the settlement output payload.
     * @param timestamp Unix timestamp associated with the signed payload.
     * @param signature Signature over the settlement payload.
     * @return settlementHash Canonical settlement hash produced by the settlement contract.
     */
    function verifySettlement(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) external returns (bytes32 settlementHash);

    /**
     * @notice Checks whether a settlement payload signature is valid.
     * @param teeId Unique identifier of the TEE instance that produced the attestation.
     * @param inputHash Hash of the settlement input payload.
     * @param outputHash Hash of the settlement output payload.
     * @param timestamp Unix timestamp associated with the signed payload.
     * @param signature Signature over the settlement payload.
     * @return True if the signature is valid for the provided payload.
     */
    function verifySignature(
        bytes32 teeId,
        bytes32 inputHash,
        bytes32 outputHash,
        uint256 timestamp,
        bytes calldata signature
    ) external view returns (bool);

}

/**
 * @title FacilitatorSettlementRelay
 * @notice Emits settlement-related events after optional signature validation.
 * @dev This relay is intended as an integration boundary between off-chain facilitators
 * and on-chain consumers. Signature validation is delegated to `SETTLEMENT_CONTRACT`.
 */
contract FacilitatorSettlementRelay is AccessControl {
    /// @notice Role identifier for accounts authorized to perform relay settlement actions.
    bytes32 public constant SETTLEMENT_RELAY_ROLE = keccak256("SETTLEMENT_RELAY_ROLE");

    /// @notice External settlement contract used for cryptographic verification.
    ISettlementContract public SETTLEMENT_CONTRACT;

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
     * @notice Initializes the relay with an external settlement verifier contract.
     * @param _settlement_contract Address of the deployed settlement contract implementation.
     */
    constructor(address _settlement_contract) {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(SETTLEMENT_RELAY_ROLE, msg.sender);

        SETTLEMENT_CONTRACT = ISettlementContract(_settlement_contract);
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
