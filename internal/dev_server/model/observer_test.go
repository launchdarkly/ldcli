package model

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type testObserver struct {
	handle func(interface{})
}

func (o testObserver) Handle(event interface{}) { o.handle(event) }

func TestObservers(t *testing.T) {
	t.Run("Register then notify yields notification", func(t *testing.T) {
		observers := NewObservers()
		observerCalled := false
		observer := testObserver{handle: func(i interface{}) {
			observerCalled = true
		}}
		observers.RegisterObserver(observer)
		observers.Notify("lol")
		assert.True(t, observerCalled, "observer should be called")
	})
	t.Run("Register, deregister then notify yields NO notification", func(t *testing.T) {
		observers := NewObservers()
		observer := testObserver{handle: func(i interface{}) {
			assert.Fail(t, "should not be called")
		}}
		id := observers.RegisterObserver(observer)
		ok := observers.DeregisterObserver(id)
		assert.True(t, ok, "observer should be deregistered")
		observers.Notify("lol")
	})
	t.Run("deregistering from multiple go routines should not panic", func(t *testing.T) {
		observers := NewObservers()
		observer := testObserver{handle: func(i interface{}) {
			assert.Fail(t, "should not be called")
		}}
		ids := make([]uuid.UUID, 100)
		for i := 0; i < 100; i++ {
			i := i
			ids[i] = observers.RegisterObserver(observer)
		}
		wg := sync.WaitGroup{}
		for _, id := range ids {
			id := id
			wg.Add(1)
			go func() {
				observers.DeregisterObserver(id)
				wg.Done()
			}()
		}
		wg.Wait()
	})
}
