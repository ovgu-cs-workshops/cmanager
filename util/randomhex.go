package util

import (
	"crypto/rand"
	"encoding/hex"
)

// RandomHex generates a randomized hex string with the specified length (in byte)
// The resulting string is 2*n in length
func RandomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
