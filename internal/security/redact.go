package security

import (
	"regexp"
	"strings"
)

type Redactor struct {
	secrets []string
}

func NewRedactor(secrets []string) Redactor {
	var filtered []string
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) >= 4 {
			filtered = append(filtered, secret)
		}
	}
	return Redactor{secrets: filtered}
}

func (r Redactor) Redact(input string) string {
	out := input
	for _, secret := range r.secrets {
		out = strings.ReplaceAll(out, secret, Mask(secret))
	}
	for _, pattern := range secretPatterns {
		out = pattern.re.ReplaceAllString(out, pattern.repl)
	}
	return out
}

func Mask(secret string) string {
	if len(secret) <= 4 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}

type redactionPattern struct {
	re   *regexp.Regexp
	repl string
}

var secretPatterns = []redactionPattern{
	{regexp.MustCompile(`(?i)(CF_Token=)[^\s]+`), `${1}****`},
	{regexp.MustCompile(`(?i)(CF_Key=)[^\s]+`), `${1}****`},
	{regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s]+`), `${1}****`},
	{regexp.MustCompile(`(?i)(token=)[^&\s]+`), `${1}****`},
}
