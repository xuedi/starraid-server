package auth

import (
	"context"
	"errors"
	"testing"
)

type authFunc func(context.Context, string, string) (Identity, error)

func (f authFunc) Authenticate(ctx context.Context, u, s string) (Identity, error) {
	return f(ctx, u, s)
}

func TestAnyFallsThroughToDev(t *testing.T) {
	reject := authFunc(func(context.Context, string, string) (Identity, error) {
		return Identity{}, ErrInvalidCredentials
	})
	a := Any{reject, Dev{User: "dev", Secret: "s3cret"}}

	id, err := a.Authenticate(context.Background(), "dev", "s3cret")
	if err != nil {
		t.Fatalf("dev creds should validate via the second authenticator: %v", err)
	}
	if id.AccountID != "dev" {
		t.Fatalf("identity = %+v, want dev account", id)
	}

	if _, err := a.Authenticate(context.Background(), "dev", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bad creds: want ErrInvalidCredentials, got %v", err)
	}
}

func TestAnyReturnsFirstSuccess(t *testing.T) {
	first := authFunc(func(context.Context, string, string) (Identity, error) {
		return Identity{AccountID: "42", ObjectID: 7}, nil
	})
	a := Any{first, Dev{User: "dev", Secret: "s3cret"}}
	id, err := a.Authenticate(context.Background(), "whoever", "whatever")
	if err != nil || id.AccountID != "42" || id.ObjectID != 7 {
		t.Fatalf("want first authenticator's identity, got %+v err=%v", id, err)
	}
}

func TestAnySurfacesInfraErrorWhenNoneAccept(t *testing.T) {
	boom := errors.New("db unreachable")
	infra := authFunc(func(context.Context, string, string) (Identity, error) {
		return Identity{}, boom
	})
	a := Any{infra, Dev{User: "dev", Secret: "s3cret"}}
	if _, err := a.Authenticate(context.Background(), "dev", "wrong"); !errors.Is(err, boom) {
		t.Fatalf("want the infrastructure error surfaced, got %v", err)
	}
}
