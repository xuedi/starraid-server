package auth

import (
	"context"
	"errors"
	"testing"
)

func TestDevAuthenticate(t *testing.T) {
	dev := Dev{User: "ada", Secret: "lovelace"}
	ctx := context.Background()

	tests := []struct {
		name, user, secret string
		wantErr            bool
	}{
		{"valid", "ada", "lovelace", false},
		{"wrong secret", "ada", "nope", true},
		{"wrong user", "babbage", "lovelace", true},
		{"empty both", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := dev.Authenticate(ctx, tt.user, tt.secret)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidCredentials) {
					t.Fatalf("want ErrInvalidCredentials, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.AccountID != "ada" || id.Username != "ada" {
				t.Fatalf("identity = %+v", id)
			}
		})
	}
}

// An empty configured secret must reject everything — never accept-all.
func TestDevUnconfiguredRejectsAll(t *testing.T) {
	var dev Dev // zero value: empty user and secret
	if _, err := dev.Authenticate(context.Background(), "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unconfigured dev auth accepted empty creds: %v", err)
	}
}
