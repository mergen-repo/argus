package apikey

import (
	"fmt"
	"net/http"

	"github.com/btopcu/argus/internal/export"
)

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	filename := export.BuildFilename("api_keys", nil)
	header := []string{"id", "name", "key_prefix", "scopes", "rate_limit_per_minute", "rate_limit_per_hour", "expires_at", "revoked_at", "last_used_at", "created_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			keys, next, err := h.apiKeyStore.ListByTenant(r.Context(), cursor, 500)
			if err != nil {
				h.logger.Error().Err(err).Msg("export api keys")
				return
			}
			for _, k := range keys {
				expiresAt := ""
				if k.ExpiresAt != nil {
					expiresAt = k.ExpiresAt.Format("2006-01-02T15:04:05Z")
				}
				revokedAt := ""
				if k.RevokedAt != nil {
					revokedAt = k.RevokedAt.Format("2006-01-02T15:04:05Z")
				}
				lastUsedAt := ""
				if k.LastUsedAt != nil {
					lastUsedAt = k.LastUsedAt.Format("2006-01-02T15:04:05Z")
				}
				scopes := ""
				for i, s := range k.Scopes {
					if i > 0 {
						scopes += "|"
					}
					scopes += s
				}
				if !yield([]string{
					k.ID.String(), k.Name, k.KeyPrefix, scopes,
					itoa(k.RateLimitPerMinute),
					itoa(k.RateLimitPerHour),
					expiresAt, revokedAt, lastUsedAt,
					k.CreatedAt.Format("2006-01-02T15:04:05Z"),
				}) {
					return
				}
			}
			if next == "" {
				break
			}
			cursor = next
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", n)
}
