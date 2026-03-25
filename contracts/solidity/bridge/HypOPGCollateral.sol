// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

import "./IInterchainGasPaymaster.sol";
import "./IMailbox.sol";
import "./IMessageRecipient.sol";
import "./ITokenBridge.sol";
import "./TokenMessage.sol";
import "./TypeCasts.sol";

/// @title HypOPGCollateral
/// @notice Reference Base-side collateral router for OPG.
/// @dev Production deployments should use Hyperlane's audited
/// HypERC20Collateral. This contract keeps the bridge architecture testable
/// from within this repo and documents the expected collateral flow.
contract HypOPGCollateral is IMessageRecipient, ITokenBridge {
    using SafeERC20 for IERC20;
    using TypeCasts for bytes32;

    uint256 public constant DESTINATION_GAS = 300_000;

    IERC20 public immutable collateralToken;
    IMailbox public immutable mailbox;
    IInterchainGasPaymaster public immutable igp;

    address public owner;
    mapping(uint32 => bytes32) public routers;

    error IncorrectMailboxFee(uint256 expected, uint256 actual);
    error InvalidAmount();
    error LengthMismatch(uint256 expected, uint256 actual);
    error NotMailbox(address caller);
    error NotOwner(address caller);
    error UnknownRouter(uint32 domain);
    error ZeroMailbox();
    error ZeroOwner();
    error ZeroRecipient();
    error ZeroRouter();
    error ZeroToken();

    event SentTransferRemote(
        bytes32 indexed messageId,
        address indexed sender,
        uint32 indexed destination,
        bytes32 recipient,
        uint256 amount,
        uint256 dispatchFee
    );
    event ReceivedTransferRemote(
        uint32 indexed origin,
        bytes32 indexed sender,
        address indexed recipient,
        uint256 amount
    );
    event RouterEnrolled(uint32 indexed domain, bytes32 router);
    event RouterUnenrolled(uint32 indexed domain);
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

    constructor(
        address _token,
        address _mailbox,
        address _igp,
        address _owner
    ) {
        if (_token == address(0)) {
            revert ZeroToken();
        }
        if (_mailbox == address(0)) {
            revert ZeroMailbox();
        }
        if (_owner == address(0)) {
            revert ZeroOwner();
        }

        collateralToken = IERC20(_token);
        mailbox = IMailbox(_mailbox);
        igp = IInterchainGasPaymaster(_igp);
        owner = _owner;
    }

    function transferRemote(
        uint32 _destination,
        bytes32 _recipient,
        uint256 _amount
    ) external payable returns (bytes32 messageId) {
        if (_amount == 0) {
            revert InvalidAmount();
        }
        if (_recipient == bytes32(0)) {
            revert ZeroRecipient();
        }

        bytes32 router = _mustHaveRemoteRouter(_destination);
        bytes memory messageBody = TokenMessage.format(_recipient, _amount);
        uint256 dispatchFee = mailbox.quoteDispatch(_destination, router, messageBody);
        if (msg.value != dispatchFee) {
            revert IncorrectMailboxFee(dispatchFee, msg.value);
        }

        collateralToken.safeTransferFrom(msg.sender, address(this), _amount);

        messageId = mailbox.dispatch{value: dispatchFee}(
            _destination,
            router,
            messageBody
        );

        emit SentTransferRemote(
            messageId,
            msg.sender,
            _destination,
            _recipient,
            _amount,
            dispatchFee
        );
    }

    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _body
    ) external payable onlyMailbox {
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

        address recipient = recipientB32.bytes32ToAddress();
        collateralToken.safeTransfer(recipient, amount);

        emit ReceivedTransferRemote(_origin, _sender, recipient, amount);
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

    function quoteHandleGasPayment(uint32 _destination) external view returns (uint256) {
        if (address(igp) == address(0)) {
            return 0;
        }

        return igp.quoteGasPayment(_destination, DESTINATION_GAS);
    }

    function quoteTransferRemote(
        uint32 _destination,
        bytes32 _recipient,
        uint256 _amount
    ) external view returns (Quote[] memory quotes) {
        uint256 dispatchFee = quoteDispatchFee(_destination, _recipient, _amount);
        quotes = new Quote[](2);
        quotes[0] = Quote({token: address(0), amount: dispatchFee});
        quotes[1] = Quote({token: address(collateralToken), amount: _amount});
    }

    function enrollRemoteRouter(uint32 _domain, bytes32 _router) external onlyOwner {
        if (_router == bytes32(0)) {
            revert ZeroRouter();
        }

        routers[_domain] = _router;
        emit RouterEnrolled(_domain, _router);
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
            if (_routers[i] == bytes32(0)) {
                revert ZeroRouter();
            }

            routers[_domains[i]] = _routers[i];
            emit RouterEnrolled(_domains[i], _routers[i]);
        }
    }

    function unenrollRemoteRouter(uint32 _domain) external onlyOwner {
        delete routers[_domain];
        emit RouterUnenrolled(_domain);
    }

    function transferOwnership(address _newOwner) external onlyOwner {
        if (_newOwner == address(0)) {
            revert ZeroOwner();
        }

        emit OwnershipTransferred(owner, _newOwner);
        owner = _newOwner;
    }

    function _mustHaveRemoteRouter(uint32 _domain) internal view returns (bytes32 router) {
        router = routers[_domain];
        if (router == bytes32(0)) {
            revert UnknownRouter(_domain);
        }
    }

    receive() external payable {}
}
