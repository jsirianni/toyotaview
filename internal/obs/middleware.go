package obs

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func Middleware(observer *Observer) func(http.Handler) http.Handler {
	if observer == nil {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			route := routePattern(r)

			ctx, span := observer.Tracer().Start(r.Context(), "http.request")
			span.SetAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", route),
			)

			recorder := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(recorder, r.WithContext(ctx))

			duration := time.Since(startedAt)
			observer.metrics.RecordHTTP(ctx, r.Method, route, recorder.statusCode, duration)
			span.SetAttributes(attribute.Int("http.status_code", recorder.statusCode))
			if recorder.statusCode >= 500 {
				span.SetStatus(codes.Error, http.StatusText(recorder.statusCode))
			}
			span.End()
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(payload []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}

	return r.ResponseWriter.Write(payload)
}

func routePattern(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}

	if r.URL == nil {
		return ""
	}

	return r.URL.Path
}
