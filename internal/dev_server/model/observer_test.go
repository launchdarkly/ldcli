package model_test

import (
	"testing"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/stretchr/testify/assert"
)

type testObserver struct {
	handle func(interface{})
}

func (o testObserver) Handle(event interface{}) { o.handle(event) }

func TestObservers(t *testing.T) {
	t.Run("Register then notify yields notification", func(t *testing.T) {
		observers := model.NewObservers()
		observerCalled := false
		observer := testObserver{handle: func(i interface{}) {
			observerCalled = true
		}}
		observers.RegisterObserver(observer)
		observers.Notify("lol")
		assert.True(t, observerCalled, "observer should be called")
	})
	t.Run("Register, deregister then notify yields NO notification", func(t *testing.T) {
		observers := model.NewObservers()
		observer := testObserver{handle: func(i interface{}) {
			assert.Fail(t, "should not be called")
		}}
		id := observers.RegisterObserver(observer)
		ok := observers.DeregisterObserver(id)
		assert.True(t, ok, "observer should be deregistered")
		observers.Notify("lol")
	})

}
