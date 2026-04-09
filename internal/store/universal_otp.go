package store

import (
	"os"
	"strings"
	"unicode"
)

const universalOTPEnvKey = "UNIVERSAL_OTP_CODE"

func isUniversalOTP(candidate string) bool {
	cfg := strings.TrimSpace(os.Getenv(universalOTPEnvKey))
	if cfg == "" {
		return false
	}
	if strings.TrimSpace(candidate) != cfg {
		return false
	}
	for _, r := range cfg {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
