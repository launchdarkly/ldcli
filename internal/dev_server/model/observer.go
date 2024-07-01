package model

import (
	"log"

	"github.com/google/uuid"
)

//go:generate go run go.uber.org/mock/mockgen -destination mock_observer/observer.go -package mock_observer . Observer

type Observer interface {
	Handle(interface{})
}

type Observers struct {
	observers map[uuid.UUID]Observer
}

func NewObservers() *Observers {
	observers := new(Observers)
	observers.observers = make(map[uuid.UUID]Observer)
	return observers
}

func (o *Observers) DeregisterObserver(observerId uuid.UUID) bool {
	log.Printf("DeregisterObserver: observer %+v", observerId)
	for key := range o.observers {
		if key == observerId {
			delete(o.observers, key)
			return true
		}
	}
	return false
}

func (o *Observers) RegisterObserver(observer Observer) uuid.UUID {
	log.Printf("RegisterObserver: observer %+v", observer)
	id := uuid.New()
	o.observers[id] = observer
	return id
}

func (o *Observers) Notify(event interface{}) {
	log.Printf("Notify: event %+v to %d observers", event, len(o.observers))
	for _, observer := range o.observers {
		observer.Handle(event)
	}
}
