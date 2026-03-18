package mocks

import (
	mock "github.com/stretchr/testify/mock"

	types "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper is a mock type for the AccountKeeper interface.
type AccountKeeper struct {
	mock.Mock
}

// NewAccountKeeper creates a new AccountKeeper mock and registers cleanup.
func NewAccountKeeper(t interface{ Cleanup(func()) }) *AccountKeeper {
	m := &AccountKeeper{}
	m.Mock.Test(nil)
	t.Cleanup(func() { m.AssertExpectations(nil) })
	return m
}

func (_m *AccountKeeper) GetModuleAddress(moduleName string) types.AccAddress {
	ret := _m.Called(moduleName)
	return ret.Get(0).(types.AccAddress)
}
