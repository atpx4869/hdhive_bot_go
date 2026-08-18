package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	cipher, err := New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte(`{"cookie":"secret"}`)
	first, err := cipher.Encrypt(123, plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	second, err := cipher.Encrypt(123, plaintext)
	if err != nil {
		t.Fatalf("Encrypt() second error = %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("Encrypt() reused a nonce")
	}

	got, err := cipher.Decrypt(123, first)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt() = %q, want %q", got, plaintext)
	}
}

func TestDecryptRejectsTamperingAndWrongAAD(t *testing.T) {
	cipher, err := New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt(123, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), ciphertext...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := cipher.Decrypt(123, tampered); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("tampered Decrypt() error = %v", err)
	}
	if _, err := cipher.Decrypt(456, ciphertext); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("wrong-AAD Decrypt() error = %v", err)
	}
	if _, err := cipher.Decrypt(123, []byte("short")); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("short Decrypt() error = %v", err)
	}
}

func TestNewRejectsWrongKeyLength(t *testing.T) {
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("New() unexpectedly accepted a short key")
	}
}
