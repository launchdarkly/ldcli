package model

import (
	"context"
	"net/http"
)

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
