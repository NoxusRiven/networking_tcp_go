package cryptography

import (
	"crypto/rand"
	"encoding/hex"
)

type ByteCount int

const (
	MESSAGE_NODE  ByteCount = 4
	INSTANCE_NODE ByteCount = 8
	CONN          ByteCount = 8
)

func GenerateID(bc ByteCount) string {
	b := make([]byte, bc)
	rand.Read(b)
	return hex.EncodeToString(b)
}
