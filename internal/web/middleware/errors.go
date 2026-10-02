package middleware

import (
	"net/http"
)

// ErrorPageRenderer is the subset of render.Renderer used by the error
// middleware. Declared here to avoid an import cycle.
type ErrorPageRenderer interface {
	NotFound(w http.ResponseWriter, req *http.Request)
	ServerError(w http.ResponseWriter, req *http.Request)
}

// NotFoundInterceptor wraps a handler and detects the default 404 response
// from http.ServeMux. When a 404 is detected with the standard plain-text
// body, it delegates to the renderer's NotFound page.
//
// "Standard plain-text body" is the string "404 page not found\n", which
// http.ServeMux writes automatically. Any handler that writes its own 404
// body (via http.Error or http.NotFound) also produces this body, so we
// can't distinguish — but rendering a designed 404 for all of them is
// exactly what we want.
func NotFoundInterceptor(r ErrorPageRenderer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// Intercept the response to inspect status + body.
			rec := &interceptWriter{ResponseWriter: w}
			next.ServeHTTP(rec, req)

			if rec.status == http.StatusNotFound && !rec.wroteBody {
				// Nothing was written. Render our 404.
				r.NotFound(w, req)
			}
		})
	}
}

// interceptWriter captures the status code and whether a body was written.
type interceptWriter struct {
	http.ResponseWriter
	status    int
	wroteBody bool
	wroteHead bool
}

func (i *interceptWriter) WriteHeader(code int) {
	if !i.wroteHead {
		i.status = code
		i.wroteHead = true
	}
	i.ResponseWriter.WriteHeader(code)
}

func (i *interceptWriter) Write(b []byte) (int, error) {
	if !i.wroteHead {
		i.status = http.StatusOK
		i.wroteHead = true
	}
	// http.ServeMux's default 404 writes "404 page not found\n". We treat
	// that as "nothing custom was written" by not forwarding it and letting
	// the middleware render its own page.
	if i.status == http.StatusNotFound && string(b) == "404 page not found\n" {
		return len(b), nil // swallow
	}
	i.wroteBody = true
	return i.ResponseWriter.Write(b)
}
