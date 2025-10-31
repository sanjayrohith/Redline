package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword returns a bcrypt hash of plain, suitable for storing in
// User.PasswordHash.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword reports whether plain matches hash. A user with an empty
// hash (created without a password) never verifies, regardless of plain.
func VerifyPassword(hash, plain string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
