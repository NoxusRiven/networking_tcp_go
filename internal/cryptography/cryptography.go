package cryptography

import (
	"crypto/rand"
	"encoding/hex"
)

func GenerateID() string {
	b := make([]byte, 7)
	rand.Read(b)
	return hex.EncodeToString(b)
}
