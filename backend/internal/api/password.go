package api

import (
	"golang.org/x/crypto/bcrypt"
)

// maxPasswordLen is bcrypt's input limit; it would silently ignore the rest, so longer passwords are refused.
const maxPasswordLen = 72

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
