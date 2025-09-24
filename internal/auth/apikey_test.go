package auth

import "testing"

func TestGenerateAPIKey(t *testing.T) {
	key, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	if len(key.Plaintext) == 0 {
		t.Fatal("Plaintext is empty")
	}
	if key.Plaintext[:3] != "rl_" {
		t.Errorf("Plaintext = %q, want rl_ prefix", key.Plaintext)
	}
	if key.DisplayPrefix != key.Plaintext[:len(key.DisplayPrefix)] {
		t.Error("DisplayPrefix is not a prefix of Plaintext")
	}
	if key.Hash == key.Plaintext {
		t.Error("Hash must never equal Plaintext")
	}
}

func TestGenerateAPIKey_UniquePerCall(t *testing.T) {
	first, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}
	second, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	if first.Plaintext == second.Plaintext {
		t.Error("two calls produced the same plaintext key")
	}
	if first.Hash == second.Hash {
		t.Error("two calls produced the same hash (salt not randomized?)")
	}
}

func TestVerifyAPIKey(t *testing.T) {
	key, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	ok, err := VerifyAPIKey(key.Plaintext, key.Hash)
	if err != nil {
		t.Fatalf("VerifyAPIKey() error = %v", err)
	}
	if !ok {
		t.Error("VerifyAPIKey() = false, want true for the correct key")
	}

	ok, err = VerifyAPIKey("rl_wrong-key", key.Hash)
	if err != nil {
		t.Fatalf("VerifyAPIKey() error = %v", err)
	}
	if ok {
		t.Error("VerifyAPIKey() = true, want false for the wrong key")
	}
}

func TestVerifyAPIKey_RejectsMalformedHash(t *testing.T) {
	if _, err := VerifyAPIKey("rl_anything", "not-a-valid-hash"); err == nil {
		t.Fatal("VerifyAPIKey() error = nil, want error for malformed hash")
	}
}
