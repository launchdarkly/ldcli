package model

import (
	"context"
	"log"
	"net/http"
	"slices"
)

type Observer interface {
	Handle(interface{})
}

type Observers struct {
	observers []Observer
}

func NewObservers() *Observers {
	return new(Observers)
}

const observersKey = ctxKey("model.observers")

func SetObserversOnContext(ctx context.Context, observers *Observers) context.Context {
	return context.WithValue(ctx, observersKey, observers)
}
func GetObserversFromContext(ctx context.Context) *Observers {
	return ctx.Value(observersKey).(*Observers)
}
func ObserversMiddleware(observers *Observers) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			ctx = SetObserversOnContext(ctx, observers)
			r = r.WithContext(ctx)
			handler.ServeHTTP(w, r)
		})
	}
}

func (o *Observers) DeregisterObserver(observer Observer) bool {
	log.Printf("DeregisterObserver: observer %+v", observer)
	indexToDeregister := -1
loop:
	for i, knownObserver := range o.observers {
		if observer == knownObserver {
			indexToDeregister = i
			break loop
		}
	}
	if indexToDeregister != -1 {
		return false
	}
	o.observers = slices.Delete(o.observers, indexToDeregister, indexToDeregister)
	return true
}

func (o *Observers) RegisterObserver(observer Observer) {
	log.Printf("RegisterObserver: observer %+v", observer)
	o.observers = append(o.observers, observer)
}

func (o *Observers) Notify(event interface{}) {
	log.Printf("Notify: event %+v to %d observers", event, len(o.observers))
	for _, observer := range o.observers {
		observer.Handle(event)
	}
}
