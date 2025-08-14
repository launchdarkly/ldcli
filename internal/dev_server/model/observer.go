package model

import (
	"sync"

	"github.com/google/uuid"
)

//go:generate go run go.uber.org/mock/mockgen -destination mocks/observer.go -package mocks . Observer

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
	_, exists := o.observers.LoadAndDelete(observerId)
	return exists
}

func (o *Observers) RegisterObserver(observer Observer) uuid.UUID {
	id := uuid.New()
	o.observers.Store(id, observer)
	return id
}

func (o *Observers) Notify(event interface{}) {
	o.observers.Range(func(_, observer any) bool {
		observer.(Observer).Handle(event)
		return true
	})
}
