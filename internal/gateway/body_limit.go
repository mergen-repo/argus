package gateway

import (
	"fmt"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
)

func BodyLimit(mb int) func(http.Handler) http.Handler {
	maxBytes := int64(mb) * 1024 * 1024
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				apierr.WriteError(w, http.StatusRequestEntityTooLarge,
					apierr.CodeRequestBodyTooLarge,
					fmt.Sprintf("Request body exceeds limit of %d MB", mb),
				)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
