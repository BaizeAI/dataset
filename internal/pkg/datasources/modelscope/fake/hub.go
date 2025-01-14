// Code generated by counterfeiter. DO NOT EDIT.
package fake

import (
	"context"
	"sync"

	"github.com/BaizeAI/dataset/internal/pkg/datasources/modelscope"
)

type FakeHubAPI struct {
	LoginStub        func(context.Context, string) (*modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse], error)
	loginMutex       sync.RWMutex
	loginArgsForCall []struct {
		arg1 context.Context
		arg2 string
	}
	loginReturns struct {
		result1 *modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse]
		result2 error
	}
	loginReturnsOnCall map[int]struct {
		result1 *modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse]
		result2 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeHubAPI) Login(arg1 context.Context, arg2 string) (*modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse], error) {
	fake.loginMutex.Lock()
	ret, specificReturn := fake.loginReturnsOnCall[len(fake.loginArgsForCall)]
	fake.loginArgsForCall = append(fake.loginArgsForCall, struct {
		arg1 context.Context
		arg2 string
	}{arg1, arg2})
	stub := fake.LoginStub
	fakeReturns := fake.loginReturns
	fake.recordInvocation("Login", []interface{}{arg1, arg2})
	fake.loginMutex.Unlock()
	if stub != nil {
		return stub(arg1, arg2)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	return fakeReturns.result1, fakeReturns.result2
}

func (fake *FakeHubAPI) LoginCallCount() int {
	fake.loginMutex.RLock()
	defer fake.loginMutex.RUnlock()
	return len(fake.loginArgsForCall)
}

func (fake *FakeHubAPI) LoginCalls(stub func(context.Context, string) (*modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse], error)) {
	fake.loginMutex.Lock()
	defer fake.loginMutex.Unlock()
	fake.LoginStub = stub
}

func (fake *FakeHubAPI) LoginArgsForCall(i int) (context.Context, string) {
	fake.loginMutex.RLock()
	defer fake.loginMutex.RUnlock()
	argsForCall := fake.loginArgsForCall[i]
	return argsForCall.arg1, argsForCall.arg2
}

func (fake *FakeHubAPI) LoginReturns(result1 *modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse], result2 error) {
	fake.loginMutex.Lock()
	defer fake.loginMutex.Unlock()
	fake.LoginStub = nil
	fake.loginReturns = struct {
		result1 *modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse]
		result2 error
	}{result1, result2}
}

func (fake *FakeHubAPI) LoginReturnsOnCall(i int, result1 *modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse], result2 error) {
	fake.loginMutex.Lock()
	defer fake.loginMutex.Unlock()
	fake.LoginStub = nil
	if fake.loginReturnsOnCall == nil {
		fake.loginReturnsOnCall = make(map[int]struct {
			result1 *modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse]
			result2 error
		})
	}
	fake.loginReturnsOnCall[i] = struct {
		result1 *modelscope.HubAPIBaseResponse[modelscope.HubAPILoginResponse]
		result2 error
	}{result1, result2}
}

func (fake *FakeHubAPI) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.loginMutex.RLock()
	defer fake.loginMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeHubAPI) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ modelscope.HubAPI = new(FakeHubAPI)
