package auth_test

import (
	"testing"

	"github.com/sanjayrohith/redline/internal/auth"
)

func TestHashPassword_VerifyPasswordRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("s3cret!")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == "s3cret!" {
		t.Fatal("HashPassword() returned the plaintext unchanged")
	}
	if !auth.VerifyPassword(hash, "s3cret!") {
		t.Error("VerifyPassword() = false, want true for the matching password")
	}
	if auth.VerifyPassword(hash, "wrong") {
		t.Error("VerifyPassword() = true, want false for a mismatched password")
	}
}

func TestVerifyPassword_EmptyHashNeverMatches(t *testing.T) {
	if auth.VerifyPassword("", "") {
		t.Error("VerifyPassword(\"\", \"\") = true, want false: an empty hash must never verify")
	}
	if auth.VerifyPassword("", "anything") {
		t.Error("VerifyPassword() with empty hash = true, want false")
	}
}
