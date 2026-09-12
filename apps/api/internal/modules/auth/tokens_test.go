package auth

import (
	"testing"
	"time"
)

func timeNow() time.Time { return time.Now() }

func TestValidateAcrossKeyRotation(t *testing.T) {
	oldSecret := "old-secret-rotate-me"
	newSecret := "new-secret-rotate-me"

	// New deployment state: signs with the new key but still accepts the old.
	svc := NewTokenService(newSecret, nil)
	svc.AddPriorSecret(oldSecret)

	// A token minted under the OLD secret (simulated by a service that only
	// knows the old key) must still validate after rotation.
	oldSvc := NewTokenService(oldSecret, nil)
	oldToken, err := oldSvc.issueAccessToken("user-1", "USER", "alice", timeNow())
	if err != nil {
		t.Fatalf("issue old token: %v", err)
	}
	claims, err := svc.Validate(oldToken)
	if err != nil {
		t.Fatalf("old token should validate during rotation grace: %v", err)
	}
	if claims.UserID != "user-1" || claims.Role != "USER" {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	// A token minted under the NEW secret validates via the primary path.
	newToken, err := svc.issueAccessToken("user-2", "CREATOR", "bob", timeNow())
	if err != nil {
		t.Fatalf("issue new token: %v", err)
	}
	claims, err = svc.Validate(newToken)
	if err != nil {
		t.Fatalf("new token should validate: %v", err)
	}
	if claims.Username != "bob" {
		t.Fatalf("unexpected username: %q", claims.Username)
	}

	// Garbage and foreign-key tokens are rejected outright.
	if _, err := svc.Validate("not-a-token"); err == nil {
		t.Fatal("garbage token should not validate")
	}
	foreign := NewTokenService("completely-unrelated", nil)
	stranger, err := foreign.issueAccessToken("u", "USER", "eve", timeNow())
	if err != nil {
		t.Fatalf("issue foreign token: %v", err)
	}
	if _, err := svc.Validate(stranger); err == nil {
		t.Fatal("token signed by an unknown key should not validate")
	}
}

func TestAddPriorSecretIgnoresEmpty(t *testing.T) {
	svc := NewTokenService("primary", nil)
	svc.AddPriorSecret("")
	svc.AddPriorSecret("")
	if len(svc.prior) != 0 {
		t.Fatalf("empty secrets must not be registered, got %d", len(svc.prior))
	}
}
