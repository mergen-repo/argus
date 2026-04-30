package notification

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

type PasswordResetEmailData struct {
	UserName    string
	ResetURL    string
	ExpiryHuman string
}

func RenderPasswordResetEmail(data PasswordResetEmailData) (string, string, string, error) {
	subject := "Argus — Parola Sıfırlama Bağlantısı"

	txtTmpl, err := texttemplate.ParseFS(templatesFS, "templates/password_reset_email.txt.tmpl")
	if err != nil {
		return "", "", "", fmt.Errorf("parse text tmpl: %w", err)
	}
	var txtBuf bytes.Buffer
	if err := txtTmpl.Execute(&txtBuf, data); err != nil {
		return "", "", "", fmt.Errorf("exec text tmpl: %w", err)
	}

	htmlTmpl, err := htmltemplate.ParseFS(templatesFS, "templates/password_reset_email.html.tmpl")
	if err != nil {
		return "", "", "", fmt.Errorf("parse html tmpl: %w", err)
	}
	var htmlBuf bytes.Buffer
	if err := htmlTmpl.Execute(&htmlBuf, data); err != nil {
		return "", "", "", fmt.Errorf("exec html tmpl: %w", err)
	}

	return subject, txtBuf.String(), htmlBuf.String(), nil
}
