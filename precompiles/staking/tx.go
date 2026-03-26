package staking

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	cmn "github.com/cosmos/evm/precompiles/common"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const (
	// CreateValidatorMethod defines the ABI method name for the staking create validator transaction
	CreateValidatorMethod = "createValidator"
	// EditValidatorMethod defines the ABI method name for the staking edit validator transaction
	EditValidatorMethod = "editValidator"
	// DelegateMethod defines the ABI method name for the staking Delegate
	// transaction.
	DelegateMethod = "delegate"
	// DelegateToBondedValidatorsMethod defines the ABI method name for delegating
	// equally across the bonded validator set in a single transaction.
	DelegateToBondedValidatorsMethod = "delegateToBondedValidators"
	// UndelegateMethod defines the ABI method name for the staking Undelegate
	// transaction.
	UndelegateMethod = "undelegate"
	// RedelegateMethod defines the ABI method name for the staking Redelegate
	// transaction.
	RedelegateMethod = "redelegate"
	// CancelUnbondingDelegationMethod defines the ABI method name for the staking
	// CancelUnbondingDelegation transaction.
	CancelUnbondingDelegationMethod = "cancelUnbondingDelegation"
)

// CreateValidator performs create validator.
func (p Precompile) CreateValidator(
	ctx sdk.Context,
	contract *vm.Contract,
	stateDB vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	bondDenom, err := p.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return nil, err
	}
	msg, validatorHexAddr, err := NewMsgCreateValidator(args, bondDenom, p.addrCdc)
	if err != nil {
		return nil, err
	}

	p.Logger(ctx).Debug(
		"tx called",
		"method", method.Name,
		"commission", msg.Commission.String(),
		"min_self_delegation", msg.MinSelfDelegation.String(),
		"validator_address", validatorHexAddr.String(),
		"pubkey", msg.Pubkey.String(),
		"value", msg.Value.Amount.String(),
	)

	msgSender := contract.Caller()

	// We won't allow calls from smart contracts
	// unless they are EIP-7702 delegated accounts
	code := stateDB.GetCode(msgSender)
	_, delegated := ethtypes.ParseDelegation(code)
	if len(code) > 0 && !delegated {
		// call by contract method
		return nil, errors.New(ErrCannotCallFromContract)
	}

	if msgSender != validatorHexAddr {
		return nil, fmt.Errorf(cmn.ErrRequesterIsNotMsgSender, msgSender.String(), validatorHexAddr.String())
	}

	// Execute the transaction using the message server
	if _, err = p.stakingMsgServer.CreateValidator(ctx, msg); err != nil {
		return nil, err
	}

	// Here we don't add journal entries here because calls from
	// smart contracts are not supported at the moment for this method.

	// Emit the event for the create validator transaction
	if err = p.EmitCreateValidatorEvent(ctx, stateDB, msg, validatorHexAddr); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}

// EditValidator performs edit validator.
func (p Precompile) EditValidator(
	ctx sdk.Context,
	contract *vm.Contract,
	stateDB vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	msg, validatorHexAddr, err := NewMsgEditValidator(args)
	if err != nil {
		return nil, err
	}

	p.Logger(ctx).Debug(
		"tx called",
		"method", method.Name,
		"validator_address", msg.ValidatorAddress,
		"commission_rate", msg.CommissionRate,
		"min_self_delegation", msg.MinSelfDelegation,
	)

	msgSender := contract.Caller()

	// We won't allow calls from smart contracts
	// unless they are EIP-7702 delegated accounts
	code := stateDB.GetCode(msgSender)
	_, delegated := ethtypes.ParseDelegation(code)
	if len(code) > 0 && !delegated {
		// call by contract method
		return nil, errors.New(ErrCannotCallFromContract)
	}

	if msgSender != validatorHexAddr {
		return nil, fmt.Errorf(cmn.ErrRequesterIsNotMsgSender, msgSender.String(), validatorHexAddr.String())
	}

	// Execute the transaction using the message server
	if _, err = p.stakingMsgServer.EditValidator(ctx, msg); err != nil {
		return nil, err
	}

	// Emit the event for the edit validator transaction
	if err = p.EmitEditValidatorEvent(ctx, stateDB, msg, validatorHexAddr); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}

// Delegate performs a delegation of coins from a delegator to a validator.
func (p *Precompile) Delegate(
	ctx sdk.Context,
	contract *vm.Contract,
	stateDB vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	bondDenom, err := p.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return nil, err
	}
	msg, delegatorHexAddr, err := NewMsgDelegate(args, bondDenom, p.addrCdc)
	if err != nil {
		return nil, err
	}

	p.Logger(ctx).Debug(
		"tx called",
		"method", method.Name,
		"args", fmt.Sprintf(
			"{ delegator_address: %s, validator_address: %s, amount: %s }",
			delegatorHexAddr,
			msg.ValidatorAddress,
			msg.Amount.Amount,
		),
	)

	msgSender := contract.Caller()
	if msgSender != delegatorHexAddr {
		return nil, fmt.Errorf(cmn.ErrRequesterIsNotMsgSender, msgSender.String(), delegatorHexAddr.String())
	}

	// Execute the transaction using the message server
	if _, err = p.stakingMsgServer.Delegate(ctx, msg); err != nil {
		return nil, err
	}

	// Emit the event for the delegate transaction
	if err = p.EmitDelegateEvent(ctx, stateDB, msg, delegatorHexAddr); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}

// DelegateToBondedValidators delegates equally across bonded validators.
func (p *Precompile) DelegateToBondedValidators(
	ctx sdk.Context,
	contract *vm.Contract,
	stateDB vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	input, err := NewDelegateToBondedValidatorsArgs(args)
	if err != nil {
		return nil, err
	}

	msgSender := contract.Caller()
	if msgSender != input.DelegatorAddress {
		return nil, fmt.Errorf(cmn.ErrRequesterIsNotMsgSender, msgSender.String(), input.DelegatorAddress.String())
	}

	bondDenom, err := p.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return nil, err
	}

	res, err := p.stakingQuerier.Validators(ctx, &stakingtypes.QueryValidatorsRequest{
		Status: stakingtypes.BondStatusBonded,
		Pagination: &query.PageRequest{
			Limit: uint64(input.MaxValidators),
		},
	})
	if err != nil {
		return nil, err
	}
	if len(res.Validators) == 0 {
		return nil, errors.New("no bonded validators found")
	}

	delegatorAddrStr, err := p.addrCdc.BytesToString(input.DelegatorAddress.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to decode delegator address: %w", err)
	}

	validatorCount := uint32(len(res.Validators))
	baseAmount := new(big.Int).Div(input.Amount, big.NewInt(int64(validatorCount)))
	remainder := new(big.Int).Mod(input.Amount, big.NewInt(int64(validatorCount))).Uint64()

	totalDelegated := big.NewInt(0)
	validatorsUsed := uint32(0)
	for i := uint32(0); i < validatorCount; i++ {
		perValidator := new(big.Int).Set(baseAmount)
		if uint64(i) < remainder {
			perValidator = perValidator.Add(perValidator, big.NewInt(1))
		}
		// Skip zero-amount delegates (e.g. amount < validatorCount).
		if perValidator.Sign() == 0 {
			continue
		}

		msg := &stakingtypes.MsgDelegate{
			DelegatorAddress: delegatorAddrStr,
			ValidatorAddress: res.Validators[i].OperatorAddress,
			Amount: sdk.Coin{
				Denom:  bondDenom,
				Amount: math.NewIntFromBigInt(perValidator),
			},
		}

		if _, err = p.stakingMsgServer.Delegate(ctx, msg); err != nil {
			return nil, err
		}
		if err = p.EmitDelegateEvent(ctx, stateDB, msg, input.DelegatorAddress); err != nil {
			return nil, err
		}

		totalDelegated.Add(totalDelegated, perValidator)
		validatorsUsed++
	}

	p.Logger(ctx).Debug(
		"tx called",
		"method", method.Name,
		"args", fmt.Sprintf(
			"{ delegator_address: %s, amount: %s, max_validators: %d, delegated_amount: %s, validators_used: %d }",
			input.DelegatorAddress,
			input.Amount,
			input.MaxValidators,
			totalDelegated,
			validatorsUsed,
		),
	)

	return method.Outputs.Pack(totalDelegated, validatorsUsed)
}

// Undelegate performs the undelegation of coins from a validator for a delegate.
// The provided amount cannot be negative. This is validated in the msg.ValidateBasic() function.
func (p Precompile) Undelegate(
	ctx sdk.Context,
	contract *vm.Contract,
	stateDB vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	bondDenom, err := p.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return nil, err
	}
	msg, delegatorHexAddr, err := NewMsgUndelegate(args, bondDenom, p.addrCdc)
	if err != nil {
		return nil, err
	}

	p.Logger(ctx).Debug(
		"tx called",
		"method", method.Name,
		"args", fmt.Sprintf(
			"{ delegator_address: %s, validator_address: %s, amount: %s }",
			delegatorHexAddr,
			msg.ValidatorAddress,
			msg.Amount.Amount,
		),
	)

	msgSender := contract.Caller()
	if msgSender != delegatorHexAddr {
		return nil, fmt.Errorf(cmn.ErrRequesterIsNotMsgSender, msgSender.String(), delegatorHexAddr.String())
	}

	// Execute the transaction using the message server
	res, err := p.stakingMsgServer.Undelegate(ctx, msg)
	if err != nil {
		return nil, err
	}

	// Emit the event for the undelegate transaction
	if err = p.EmitUnbondEvent(ctx, stateDB, msg, delegatorHexAddr, res.CompletionTime.UTC().Unix()); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(res.CompletionTime.UTC().Unix())
}

// Redelegate performs a redelegation of coins for a delegate from a source validator
// to a destination validator.
// The provided amount cannot be negative. This is validated in the msg.ValidateBasic() function.
func (p Precompile) Redelegate(
	ctx sdk.Context,
	contract *vm.Contract,
	stateDB vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	bondDenom, err := p.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return nil, err
	}
	msg, delegatorHexAddr, err := NewMsgRedelegate(args, bondDenom, p.addrCdc)
	if err != nil {
		return nil, err
	}

	p.Logger(ctx).Debug(
		"tx called",
		"method", method.Name,
		"args", fmt.Sprintf(
			"{ delegator_address: %s, validator_src_address: %s, validator_dst_address: %s, amount: %s }",
			delegatorHexAddr,
			msg.ValidatorSrcAddress,
			msg.ValidatorDstAddress,
			msg.Amount.Amount,
		),
	)

	msgSender := contract.Caller()
	if msgSender != delegatorHexAddr {
		return nil, fmt.Errorf(cmn.ErrRequesterIsNotMsgSender, msgSender.String(), delegatorHexAddr.String())
	}

	res, err := p.stakingMsgServer.BeginRedelegate(ctx, msg)
	if err != nil {
		return nil, err
	}

	if err = p.EmitRedelegateEvent(ctx, stateDB, msg, delegatorHexAddr, res.CompletionTime.UTC().Unix()); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(res.CompletionTime.UTC().Unix())
}

// CancelUnbondingDelegation will cancel the unbonding of a delegation and delegate
// back to the validator being unbonded from.
// The provided amount cannot be negative. This is validated in the msg.ValidateBasic() function.
func (p Precompile) CancelUnbondingDelegation(
	ctx sdk.Context,
	contract *vm.Contract,
	stateDB vm.StateDB,
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	bondDenom, err := p.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return nil, err
	}
	msg, delegatorHexAddr, err := NewMsgCancelUnbondingDelegation(args, bondDenom, p.addrCdc)
	if err != nil {
		return nil, err
	}

	p.Logger(ctx).Debug(
		"tx called",
		"method", method.Name,
		"args", fmt.Sprintf(
			"{ delegator_address: %s, validator_address: %s, amount: %s, creation_height: %d }",
			delegatorHexAddr,
			msg.ValidatorAddress,
			msg.Amount.Amount,
			msg.CreationHeight,
		),
	)

	msgSender := contract.Caller()
	if msgSender != delegatorHexAddr {
		return nil, fmt.Errorf(cmn.ErrRequesterIsNotMsgSender, msgSender.String(), delegatorHexAddr.String())
	}

	if _, err = p.stakingMsgServer.CancelUnbondingDelegation(ctx, msg); err != nil {
		return nil, err
	}

	if err = p.EmitCancelUnbondingDelegationEvent(ctx, stateDB, msg, delegatorHexAddr); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(true)
}
