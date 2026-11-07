package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const encPrefix = "enc:v1:"

type Vault struct {
	key []byte
}

// NewVault initializes an AES-256-GCM credential vault using DASHBOARD_MASTER_KEY
// or a persisted 32-byte key file (.master.key) with 0600 permissions in dataDir.
func NewVault(dataDir string) (*Vault, error) {
	if envKey := os.Getenv("DASHBOARD_MASTER_KEY"); envKey != "" {
		sum := sha256.Sum256([]byte(envKey))
		return &Vault{key: sum[:]}, nil
	}

	keyPath := filepath.Join(dataDir, ".master.key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) >= 32 {
		sum := sha256.Sum256(data)
		return &Vault{key: sum[:]}, nil
	}

	rawKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, rawKey); err != nil {
		return nil, fmt.Errorf("failed to generate master key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(rawKey)
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(encoded), 0600); err != nil {
		return nil, fmt.Errorf("failed to write master key: %w", err)
	}
	sum := sha256.Sum256([]byte(encoded))
	return &Vault{key: sum[:]}, nil
}

func (v *Vault) IsEncrypted(val string) bool {
	return strings.HasPrefix(val, encPrefix)
}

func (v *Vault) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if v.IsEncrypted(plaintext) {
		return plaintext, nil
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (v *Vault) Decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !v.IsEncrypted(value) {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := raw[:ns], raw[ns:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
