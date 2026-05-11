package mocks

import (
	"context"

	mock "github.com/stretchr/testify/mock"

	types "github.com/cosmos/cosmos-sdk/types"
)

// BankKeeper is a mock type for the BankKeeper interface.
type BankKeeper struct {
	mock.Mock
}

// NewBankKeeper creates a new BankKeeper mock and registers cleanup.
func NewBankKeeper(t interface {
	mock.TestingT
	Cleanup(func())
}) *BankKeeper {
	m := &BankKeeper{}
	m.Mock.Test(t)
	t.Cleanup(func() { m.AssertExpectations(t) })
	return m
}

func (_m *BankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt types.Coins) error {
	ret := _m.Called(ctx, senderModule, recipientModule, amt)
	return ret.Error(0)
}

func (_m *BankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr types.AccAddress, recipientModule string, amt types.Coins) error {
	ret := _m.Called(ctx, senderAddr, recipientModule, amt)
	return ret.Error(0)
}

func (_m *BankKeeper) GetBalance(ctx context.Context, addr types.AccAddress, denom string) types.Coin {
	ret := _m.Called(ctx, addr, denom)
	return ret.Get(0).(types.Coin)
}
