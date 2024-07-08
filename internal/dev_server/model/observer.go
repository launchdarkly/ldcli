package model

import (
	"log"
	"sync"

	"github.com/google/uuid"
)

//go:generate go run go.uber.org/mock/mockgen -destination mock_observer/observer.go -package mock_observer . Observer

type Observer interface {
	Handle(interface{})
}

type Observers struct {
	observers sync.Map
}

func NewObservers() *Observers {
	observers := new(Observers)
	observers.observers = sync.Map{}
	return observers
}

func (o *Observers) DeregisterObserver(observerId uuid.UUID) bool {
	log.Printf("DeregisterObserver: observerId %+v", observerId)
	_, exists := o.observers.LoadAndDelete(observerId)
	return exists
}

func (o *Observers) RegisterObserver(observer Observer) uuid.UUID {
	id := uuid.New()
	log.Printf("RegisterObserver: observer %+v, id %s", observer, id)
	o.observers.Store(id, observer)
	return id
}

func (o *Observers) Notify(event interface{}) {
	log.Printf("Notify: event %+v to observers", event)
	o.observers.Range(func(_, observer any) bool {
		observer.(Observer).Handle(event)
		return true
	})
}
