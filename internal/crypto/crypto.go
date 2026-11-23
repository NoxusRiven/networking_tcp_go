package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

type ByteCount int

const (
	MESSAGE_ID ByteCount = 4

	MESSAGE_NODE  ByteCount = 4
	INSTANCE_NODE ByteCount = 8

	CONN ByteCount = 8
)

const (
	Aes256KeySize = 32
	RsaKeyBits    = 2048
)

// SealedEnvelope contains an AES-256-GCM encrypted payload and a session key
// encrypted with the recipient's RSA-OAEP public key.
type SealedEnvelope struct {
	EncryptedKey string
	Ciphertext   string
}

type KeyPayload struct {
	CliPubKey string `json:"cliPubKey,omitempty"`
	AesKey    string `json:"aesKey,omitempty"`
}

func GenerateID(bc ByteCount) string {
	b := make([]byte, bc)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateAESKey creates a new random AES session key of keySize bytes.
func GenerateAESKey() ([]byte, error) {
	key := make([]byte, Aes256KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: generate AES key: %w", err)
	}

	return key, nil
}

// EncryptSessionKey encrypts a raw symmetric session key using the recipient's
// RSA public key (OAEP with SHA-256) and returns it encoded as Base64.
func EncryptSessionKey(peerPubKey *rsa.PublicKey, sessionKey []byte) (string, error) {
	if peerPubKey == nil {
		return "", errors.New("crypto: peer public key must not be nil")
	}
	if len(sessionKey) == 0 {
		return "", errors.New("crypto: session key must not be empty")
	}

	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, peerPubKey, sessionKey, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: encrypt session key with RSA: %w", err)
	}

	return base64.StdEncoding.EncodeToString(encryptedKey), nil
}

// DecryptSessionKey decodes a Base64-encoded RSA-OAEP session key and decrypts it
// using privKey, returning the raw symmetric key bytes.
func DecryptSessionKey(privKey *rsa.PrivateKey, encodedEncryptedKey string) ([]byte, error) {
	if privKey == nil {
		return nil, errors.New("crypto: private key must not be nil")
	}
	if encodedEncryptedKey == "" {
		return nil, errors.New("crypto: encrypted key is empty")
	}

	encKeyBytes, err := base64.StdEncoding.DecodeString(encodedEncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode base64 encrypted key: %w", err)
	}

	sessionKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, encKeyBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt session key with RSA: %w", err)
	}

	return sessionKey, nil
}

// SealForPeer generates a fresh AES-256 key, encrypts plaintext with AES-GCM,
// and encrypts the AES key with peerPubKey.
func SealForPeer(peerPubKey *rsa.PublicKey, plaintext string) (SealedEnvelope, error) {
	aesKey, err := GenerateAESKey()
	if err != nil {
		return SealedEnvelope{}, err
	}

	ciphertext, err := EncryptAES(plaintext, aesKey)
	if err != nil {
		return SealedEnvelope{}, fmt.Errorf("crypto: encrypt plaintext: %w", err)
	}

	encryptedKey, err := EncryptSessionKey(peerPubKey, aesKey)
	if err != nil {
		return SealedEnvelope{}, err
	}

	return SealedEnvelope{
		EncryptedKey: encryptedKey,
		Ciphertext:   base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

// OpenFromPeer decrypts the session key using privKey and then decrypts
// the AES-GCM ciphertext contained in envelope.
func OpenFromPeer(privKey *rsa.PrivateKey, envelope SealedEnvelope) (string, error) {
	aesKey, err := DecryptSessionKey(privKey, envelope.EncryptedKey)
	if err != nil {
		return "", err
	}

	plaintext, err := DecryptAES(envelope.Ciphertext, aesKey)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt payload with AES: %w", err)
	}

	return plaintext, nil
}

// EncryptAES encrypts plaintext with AES-GCM using a randomly generated nonce
// prepended to the resulting ciphertext.
func EncryptAES(plaintext string, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: init AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: init GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// DecryptAES decodes a Base64-encoded AES-GCM ciphertext and decrypts it using key.
func DecryptAES(encodedCiphertext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: init AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: init GCM: %w", err)
	}

	raw, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return "", fmt.Errorf("crypto: decode base64 ciphertext: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("crypto: ciphertext is shorter than nonce")
	}

	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt GCM payload: %w", err)
	}

	return string(plaintext), nil
}

// MarshalPublicKeyBase64 encodes an RSA public key in PKIX DER format as Base64.
func MarshalPublicKeyBase64(pubKey *rsa.PublicKey) (string, error) {
	if pubKey == nil {
		return "", errors.New("crypto: public key must not be nil")
	}

	der, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return "", fmt.Errorf("crypto: marshal public key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(der), nil
}

// ParsePublicKeyBase64 decodes a Base64-encoded PKIX DER RSA public key.
func ParsePublicKeyBase64(encoded string) (*rsa.PublicKey, error) {
	if encoded == "" {
		return nil, errors.New("crypto: encoded public key is empty")
	}

	der, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode base64 public key: %w", err)
	}

	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("crypto: parse PKIX public key: %w", err)
	}

	pubKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("crypto: parsed key is not an RSA public key")
	}

	return pubKey, nil
}
