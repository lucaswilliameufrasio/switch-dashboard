package auth

import (
	"testing"
)

func TestVaultEncryptDecrypt(t *testing.T) {
	tmpDir := t.TempDir()

	vault, err := NewVault(tmpDir)
	if err != nil {
		t.Fatalf("NewVault failed: %v", err)
	}

	secret := "MySuperSecretSwitchPassword!2026"
	encrypted, err := vault.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if encrypted == secret {
		t.Fatalf("expected encrypted string to differ from plaintext")
	}
	if !vault.IsEncrypted(encrypted) {
		t.Fatalf("expected IsEncrypted to return true for %q", encrypted)
	}

	decrypted, err := vault.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != secret {
		t.Fatalf("Decrypt mismatch: got %q, want %q", decrypted, secret)
	}
}

func TestPasswordHashAndSessionCSRF(t *testing.T) {
	hash, err := HashPassword("SwitchAdminPass123")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !VerifyPassword(hash, "SwitchAdminPass123") {
		t.Fatalf("VerifyPassword should succeed for valid password")
	}
	if VerifyPassword(hash, "wrongpassword") {
		t.Fatalf("VerifyPassword should fail for wrong password")
	}

	mgr := NewManager()
	sess, err := mgr.CreateSession("admin")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.ID == "" || sess.CSRFToken == "" {
		t.Fatalf("expected non-empty session ID and CSRF token")
	}
}
