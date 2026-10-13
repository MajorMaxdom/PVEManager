// Package crypto seals secrets (API tokens) with AES-256-GCM using a master key file.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
)

type Box struct{ aead cipher.AEAD }

// LoadOrCreateKey reads a 32-byte key from path, creating it (0600) if missing.
func LoadOrCreateKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		if len(b) != 32 {
			return nil, errors.New("secret key must be exactly 32 bytes")
		}
		return b, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, os.WriteFile(path, k, 0o600)
}

func New(key []byte) (*Box, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return &Box{g}, nil
}

func (b *Box) Seal(plain []byte) ([]byte, error) {
	n := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return b.aead.Seal(n, n, plain, nil), nil
}

func (b *Box) Open(sealed []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(sealed) < ns {
		return nil, errors.New("sealed value too short")
	}
	return b.aead.Open(nil, sealed[:ns], sealed[ns:], nil)
}
