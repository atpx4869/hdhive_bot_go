package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
)

var ErrInvalidCiphertext = errors.New("invalid ciphertext")

// Cipher encrypts secrets with AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// New creates a Cipher. key must contain exactly 32 bytes.
func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("AES-256 key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt encrypts plaintext and binds it to userID as authenticated data.
// The returned value is nonce || sealed ciphertext.
func (c *Cipher) Encrypt(userID int64, plaintext []byte) ([]byte, error) {
	if userID <= 0 {
		return nil, errors.New("userID must be positive")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	out := make([]byte, 0, len(nonce)+len(plaintext)+c.aead.Overhead())
	out = append(out, nonce...)
	out = c.aead.Seal(out, nonce, plaintext, aad(userID))
	return out, nil
}

// Decrypt decrypts ciphertext only when it is authentic and bound to userID.
func (c *Cipher) Decrypt(userID int64, ciphertext []byte) ([]byte, error) {
	if userID <= 0 {
		return nil, errors.New("userID must be positive")
	}
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < nonceSize+c.aead.Overhead() {
		return nil, ErrInvalidCiphertext
	}
	plaintext, err := c.aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], aad(userID))
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	return plaintext, nil
}

func aad(userID int64) []byte {
	value := make([]byte, 8)
	binary.BigEndian.PutUint64(value, uint64(userID))
	return value
}
