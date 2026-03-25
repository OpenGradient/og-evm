// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import "../IBridgeMint.sol";
import "./IInterchainGasPaymaster.sol";
import "./IInterchainSecurityModule.sol";
import "./IMailbox.sol";
import "./IMessageRecipient.sol";
import "./ISpecifiesInterchainSecurityModule.sol";
import "./ITokenBridge.sol";
import "./TokenMessage.sol";
import "./TypeCasts.sol";

contract HypOGNative is
    ITokenBridge,
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule
{
    using TypeCasts for bytes32;

    IMailbox public immutable mailbox;
    IInterchainGasPaymaster public immutable igp;

    address public owner;
    IInterchainSecurityModule public ism;

    mapping(uint32 => bytes32) public routers;
    mapping(uint32 => uint256) public destinationGas;

    error BridgeDisabled();
    error DestinationGasNotConfigured(uint32 domain);
    error IGPNotConfigured();
    error IncorrectValue(uint256 expected, uint256 actual);
    error InvalidAmount();
    error LengthMismatch(uint256 expected, uint256 actual);
    error MintFailed();
    error BurnFailed();
    error NotMailbox(address caller);
    error NotOwner(address caller);
    error UnknownRouter(uint32 domain);
    error ZeroMailbox();
    error ZeroOwner();
    error ZeroRecipient();
    error ZeroRouter();

    event SentTransferRemote(
        uint32 indexed destination,
        bytes32 indexed recipient,
        uint256 amount
    );
    event ReceivedTransferRemote(
        uint32 indexed origin,
        bytes32 indexed recipient,
        uint256 amount
    );
    event RouterEnrolled(uint32 indexed domain, bytes32 router);
    event RouterUnenrolled(uint32 indexed domain);
    event DestinationGasSet(uint32 indexed domain, uint256 gasLimit);
    event HandleGasPaymentMade(
        bytes32 indexed messageId,
        uint32 indexed destination,
        uint256 gasLimit,
        uint256 payment
    );
    event ISMSet(address indexed ism);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    modifier onlyOwner() {
        if (msg.sender != owner) {
            revert NotOwner(msg.sender);
        }
        _;
    }

    modifier onlyMailbox() {
        if (msg.sender != address(mailbox)) {
            revert NotMailbox(msg.sender);
        }
        _;
    }

    constructor(address _mailbox, address _igp, address _owner) {
        if (_mailbox == address(0)) {
            revert ZeroMailbox();
        }
        if (_owner == address(0)) {
            revert ZeroOwner();
        }

        mailbox = IMailbox(_mailbox);
        igp = IInterchainGasPaymaster(_igp);
        owner = _owner;

        emit OwnershipTransferred(address(0), _owner);
    }

    function transferRemote(
        uint32 _destination,
        bytes32 _recipient,
        uint256 _amount
    ) external payable override returns (bytes32 messageId) {
        if (_amount == 0) {
            revert InvalidAmount();
        }
        if (_recipient == bytes32(0)) {
            revert ZeroRecipient();
        }
        if (!IBRIDGE_MINT_CONTRACT.isEnabled()) {
            revert BridgeDisabled();
        }

        bytes32 router = _mustHaveRemoteRouter(_destination);
        bytes memory messageBody = TokenMessage.format(_recipient, _amount);
        uint256 dispatchFee = mailbox.quoteDispatch(
            _destination,
            router,
            messageBody
        );
        uint256 expectedValue = _amount + dispatchFee;
        if (msg.value != expectedValue) {
            revert IncorrectValue(expectedValue, msg.value);
        }

        if (!IBRIDGE_MINT_CONTRACT.burnNative(_amount)) {
            revert BurnFailed();
        }

        messageId = mailbox.dispatch{value: dispatchFee}(
            _destination,
            router,
            messageBody
        );

        emit SentTransferRemote(_destination, _recipient, _amount);
    }

    function quoteDispatchFee(
        uint32 _destination,
        bytes32 _recipient,
        uint256 _amount
    ) public view returns (uint256) {
        if (_amount == 0) {
            revert InvalidAmount();
        }
        if (_recipient == bytes32(0)) {
            revert ZeroRecipient();
        }

        return mailbox.quoteDispatch(
            _destination,
            _mustHaveRemoteRouter(_destination),
            TokenMessage.format(_recipient, _amount)
        );
    }

    function quoteTransferRemote(
        uint32 _destination,
        bytes32 _recipient,
        uint256 _amount
    ) external view override returns (Quote[] memory quotes) {
        uint256 dispatchFee = quoteDispatchFee(_destination, _recipient, _amount);

        quotes = new Quote[](2);
        quotes[0] = Quote({token: address(0), amount: dispatchFee});
        quotes[1] = Quote({token: address(0), amount: _amount});
    }

    function quoteHandleGasPayment(
        uint32 _destination
    ) external view returns (uint256) {
        if (address(igp) == address(0)) {
            return 0;
        }

        uint256 gasLimit = destinationGas[_destination];
        if (gasLimit == 0) {
            return 0;
        }

        return igp.quoteGasPayment(_destination, gasLimit);
    }

    function payForHandleGas(
        bytes32 _messageId,
        uint32 _destination,
        address _refundAddress
    ) external payable {
        if (address(igp) == address(0)) {
            revert IGPNotConfigured();
        }

        uint256 gasLimit = destinationGas[_destination];
        if (gasLimit == 0) {
            revert DestinationGasNotConfigured(_destination);
        }

        igp.payForGas{value: msg.value}(
            _messageId,
            _destination,
            gasLimit,
            _refundAddress
        );

        emit HandleGasPaymentMade(_messageId, _destination, gasLimit, msg.value);
    }

    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _body
    ) external payable override onlyMailbox {
        bytes32 router = _mustHaveRemoteRouter(_origin);
        if (router != _sender) {
            revert UnknownRouter(_origin);
        }

        bytes32 recipientB32 = TokenMessage.recipient(_body);
        if (recipientB32 == bytes32(0)) {
            revert ZeroRecipient();
        }

        uint256 amount = TokenMessage.amount(_body);
        if (amount == 0) {
            revert InvalidAmount();
        }

        if (!IBRIDGE_MINT_CONTRACT.mintNative(recipientB32.bytes32ToAddress(), amount)) {
            revert MintFailed();
        }

        emit ReceivedTransferRemote(_origin, recipientB32, amount);
    }

    function enrollRemoteRouter(uint32 _domain, bytes32 _router) external onlyOwner {
        _enrollRemoteRouter(_domain, _router);
    }

    function enrollRemoteRouters(
        uint32[] calldata _domains,
        bytes32[] calldata _routers
    ) external onlyOwner {
        if (_domains.length != _routers.length) {
            revert LengthMismatch(_domains.length, _routers.length);
        }

        uint256 length = _domains.length;
        for (uint256 i = 0; i < length; ++i) {
            _enrollRemoteRouter(_domains[i], _routers[i]);
        }
    }

    function unenrollRemoteRouter(uint32 _domain) external onlyOwner {
        delete routers[_domain];
        emit RouterUnenrolled(_domain);
    }

    function setDestinationGas(uint32 _domain, uint256 _gasLimit) external onlyOwner {
        destinationGas[_domain] = _gasLimit;
        emit DestinationGasSet(_domain, _gasLimit);
    }

    function setInterchainSecurityModule(address _ism) external onlyOwner {
        ism = IInterchainSecurityModule(_ism);
        emit ISMSet(_ism);
    }

    function transferOwnership(address _newOwner) external onlyOwner {
        if (_newOwner == address(0)) {
            revert ZeroOwner();
        }

        emit OwnershipTransferred(owner, _newOwner);
        owner = _newOwner;
    }

    function interchainSecurityModule()
        external
        view
        override
        returns (IInterchainSecurityModule)
    {
        return ism;
    }

    function _enrollRemoteRouter(uint32 _domain, bytes32 _router) internal {
        if (_router == bytes32(0)) {
            revert ZeroRouter();
        }

        routers[_domain] = _router;
        emit RouterEnrolled(_domain, _router);
    }

    function _mustHaveRemoteRouter(uint32 _domain) internal view returns (bytes32 router) {
        router = routers[_domain];
        if (router == bytes32(0)) {
            revert UnknownRouter(_domain);
        }
    }

    receive() external payable {}
}
