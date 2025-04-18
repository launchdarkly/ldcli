// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/launchdarkly/ldcli/internal/dev_server/adapters (interfaces: Api)
//
// Generated by this command:
//
//	mockgen -destination mocks/api.go -package mocks . Api
//

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	gomock "go.uber.org/mock/gomock"
)

// MockApi is a mock of Api interface.
type MockApi struct {
	ctrl     *gomock.Controller
	recorder *MockApiMockRecorder
}

// MockApiMockRecorder is the mock recorder for MockApi.
type MockApiMockRecorder struct {
	mock *MockApi
}

// NewMockApi creates a new mock instance.
func NewMockApi(ctrl *gomock.Controller) *MockApi {
	mock := &MockApi{ctrl: ctrl}
	mock.recorder = &MockApiMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockApi) EXPECT() *MockApiMockRecorder {
	return m.recorder
}

// GetAllFlags mocks base method.
func (m *MockApi) GetAllFlags(arg0 context.Context, arg1 string) ([]ldapi.FeatureFlag, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAllFlags", arg0, arg1)
	ret0, _ := ret[0].([]ldapi.FeatureFlag)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetAllFlags indicates an expected call of GetAllFlags.
func (mr *MockApiMockRecorder) GetAllFlags(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAllFlags", reflect.TypeOf((*MockApi)(nil).GetAllFlags), arg0, arg1)
}

// GetProjectEnvironments mocks base method.
func (m *MockApi) GetProjectEnvironments(arg0 context.Context, arg1, arg2 string, arg3 *int) ([]ldapi.Environment, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetProjectEnvironments", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].([]ldapi.Environment)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetProjectEnvironments indicates an expected call of GetProjectEnvironments.
func (mr *MockApiMockRecorder) GetProjectEnvironments(arg0, arg1, arg2, arg3 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetProjectEnvironments", reflect.TypeOf((*MockApi)(nil).GetProjectEnvironments), arg0, arg1, arg2, arg3)
}

// GetSdkKey mocks base method.
func (m *MockApi) GetSdkKey(arg0 context.Context, arg1, arg2 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetSdkKey", arg0, arg1, arg2)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetSdkKey indicates an expected call of GetSdkKey.
func (mr *MockApiMockRecorder) GetSdkKey(arg0, arg1, arg2 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetSdkKey", reflect.TypeOf((*MockApi)(nil).GetSdkKey), arg0, arg1, arg2)
}
