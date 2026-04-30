package notification

import (
	"strings"
	"testing"
)

func TestRenderPasswordResetEmail_ContainsLinkAndExpiry(t *testing.T) {
	subject, txt, html, err := RenderPasswordResetEmail(PasswordResetEmailData{
		UserName:    "alice",
		ResetURL:    "https://example.com/reset?token=abc",
		ExpiryHuman: "1 saat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if subject == "" {
		t.Fatal("empty subject")
	}
	for _, needle := range []string{"alice", "https://example.com/reset?token=abc", "1 saat"} {
		if !strings.Contains(txt, needle) {
			t.Errorf("txt missing %q", needle)
		}
		if !strings.Contains(html, needle) {
			t.Errorf("html missing %q", needle)
		}
	}
}
