package mocks

import (
	mock "github.com/stretchr/testify/mock"

	types "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper is a mock type for the bridge AccountKeeper interface.
type AccountKeeper struct {
	mock.Mock
}

// NewAccountKeeper creates a new AccountKeeper mock and registers cleanup.
func NewAccountKeeper(t interface {
	mock.TestingT
	Cleanup(func())
}) *AccountKeeper {
	m := &AccountKeeper{}
	m.Mock.Test(t)
	t.Cleanup(func() { m.AssertExpectations(t) })
	return m
}

func (_m *AccountKeeper) GetModuleAddress(moduleName string) types.AccAddress {
	ret := _m.Called(moduleName)
	return ret.Get(0).(types.AccAddress)
}
