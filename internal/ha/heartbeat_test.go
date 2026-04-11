package ha

import "testing"

func TestValidateSecret(t *testing.T) {
	if err := validateSecret("", "anything"); err != nil {
		t.Fatalf("expected empty secret to allow peers, got %v", err)
	}
	if err := validateSecret("shared", "shared"); err != nil {
		t.Fatalf("expected matching secret to validate, got %v", err)
	}
	if err := validateSecret("shared", "wrong"); err == nil {
		t.Fatalf("expected mismatched secret to fail")
	}
}
