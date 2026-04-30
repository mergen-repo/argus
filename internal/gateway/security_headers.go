package gateway

import (
	"net/http"
)

type SecurityHeadersConfig struct {
	HSTSMaxAge            int
	HSTSIncludeSubdomains bool
	HSTSPreload           bool
	HSTSOnlyWhenTLS       bool
	TrustForwardedProto   bool
	FrameOptions          string
	ContentTypeNoSniff    bool
	XSSProtection         string
	CSPDirectives         string
	ReferrerPolicy        string
	PermissionsPolicy     string
}

func DefaultSecurityHeadersConfig() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		HSTSMaxAge:            31536000,
		HSTSIncludeSubdomains: true,
		HSTSPreload:           false,
		HSTSOnlyWhenTLS:       true,
		TrustForwardedProto:   false,
		FrameOptions:          "DENY",
		ContentTypeNoSniff:    true,
		XSSProtection:         "1; mode=block",
		CSPDirectives:         "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' wss:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		PermissionsPolicy:     "geolocation=(), microphone=(), camera=()",
	}
}

func SecurityHeaders(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.ContentTypeNoSniff {
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}

			if cfg.FrameOptions != "" {
				w.Header().Set("X-Frame-Options", cfg.FrameOptions)
			}

			if cfg.XSSProtection != "" {
				w.Header().Set("X-XSS-Protection", cfg.XSSProtection)
			}

			if cfg.CSPDirectives != "" {
				w.Header().Set("Content-Security-Policy", cfg.CSPDirectives)
			}

			if cfg.HSTSMaxAge > 0 {
				emitHSTS := !cfg.HSTSOnlyWhenTLS ||
					r.TLS != nil ||
					(cfg.TrustForwardedProto && r.Header.Get("X-Forwarded-Proto") == "https")

				if emitHSTS {
					hsts := "max-age=" + itoa(cfg.HSTSMaxAge)
					if cfg.HSTSIncludeSubdomains {
						hsts += "; includeSubDomains"
					}
					if cfg.HSTSPreload {
						hsts += "; preload"
					}
					w.Header().Set("Strict-Transport-Security", hsts)
				}
			}

			if cfg.ReferrerPolicy != "" {
				w.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
			}

			if cfg.PermissionsPolicy != "" {
				w.Header().Set("Permissions-Policy", cfg.PermissionsPolicy)
			}

			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")

			next.ServeHTTP(w, r)
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
