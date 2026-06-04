package security

import "testing"

func TestRedactorMasksExplicitSecrets(t *testing.T) {
	redactor := NewRedactor([]string{"abc123456789"})
	got := redactor.Redact("using abc123456789")
	if got != "using ****6789" {
		t.Fatalf("unexpected redaction: %q", got)
	}
}

func TestRedactorMasksKnownPatterns(t *testing.T) {
	redactor := NewRedactor(nil)
	got := redactor.Redact("CF_Token=plain Authorization: Bearer other")
	want := "CF_Token=**** Authorization: Bearer ****"
	if got != want {
		t.Fatalf("unexpected redaction: %q", got)
	}
}
