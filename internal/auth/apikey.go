// Package auth generates and verifies opaque API keys, storing only their
// Argon2id hash so a database leak never yields a usable credential.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	keyPrefix          = "rl_"
	keyRandomBytes     = 32
	displayPrefixChars = 8
)

type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultParams = argon2Params{
	memory:      64 * 1024,
	iterations:  3,
	parallelism: 2,
	saltLength:  16,
	keyLength:   32,
}

// GeneratedKey is a freshly minted API key. Plaintext is shown to the
// caller exactly once and never stored; only Hash is persisted.
type GeneratedKey struct {
	Plaintext     string
	DisplayPrefix string
	Hash          string
}

// GenerateAPIKey creates a new random API key, hashing it with Argon2id.
func GenerateAPIKey() (*GeneratedKey, error) {
	raw := make([]byte, keyRandomBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("auth: generate random bytes: %w", err)
	}

	plaintext := keyPrefix + base64.RawURLEncoding.EncodeToString(raw)

	displayPrefix := plaintext
	if cut := len(keyPrefix) + displayPrefixChars; len(displayPrefix) > cut {
		displayPrefix = plaintext[:cut]
	}

	hash, err := hashKey(plaintext)
	if err != nil {
		return nil, err
	}

	return &GeneratedKey{Plaintext: plaintext, DisplayPrefix: displayPrefix, Hash: hash}, nil
}

func hashKey(plaintext string) (string, error) {
	salt := make([]byte, defaultParams.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(plaintext), salt,
		defaultParams.iterations, defaultParams.memory, defaultParams.parallelism, defaultParams.keyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, defaultParams.memory, defaultParams.iterations, defaultParams.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyAPIKey reports whether plaintext hashes to encodedHash, comparing
// the derived hash in constant time.
func VerifyAPIKey(plaintext, encodedHash string) (bool, error) {
	params, salt, hash, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	candidate := argon2.IDKey([]byte(plaintext), salt,
		params.iterations, params.memory, params.parallelism, uint32(len(hash))) // #nosec G115 -- decoded digest length is always small

	return subtle.ConstantTimeCompare(candidate, hash) == 1, nil
}

func decodeHash(encoded string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, fmt.Errorf("auth: malformed hash encoding")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("auth: malformed hash version: %w", err)
	}
	if version != argon2.Version {
		return argon2Params{}, nil, nil, fmt.Errorf("auth: unsupported argon2 version %d", version)
	}

	params, err := parseParams(parts[3])
	if err != nil {
		return argon2Params{}, nil, nil, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("auth: malformed hash salt: %w", err)
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2Params{}, nil, nil, fmt.Errorf("auth: malformed hash digest: %w", err)
	}

	return params, salt, hash, nil
}

func parseParams(field string) (argon2Params, error) {
	var params argon2Params
	for _, kv := range strings.Split(field, ",") {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return argon2Params{}, fmt.Errorf("auth: malformed hash params")
		}

		switch key {
		case "m":
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return argon2Params{}, fmt.Errorf("auth: malformed hash param %q: %w", key, err)
			}
			params.memory = uint32(n)
		case "t":
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return argon2Params{}, fmt.Errorf("auth: malformed hash param %q: %w", key, err)
			}
			params.iterations = uint32(n)
		case "p":
			n, err := strconv.ParseUint(value, 10, 8)
			if err != nil {
				return argon2Params{}, fmt.Errorf("auth: malformed hash param %q: %w", key, err)
			}
			params.parallelism = uint8(n)
		default:
			return argon2Params{}, fmt.Errorf("auth: unknown hash param %q", key)
		}
	}
	return params, nil
}
