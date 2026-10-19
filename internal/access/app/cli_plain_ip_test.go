package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/qianlan33333-png/AI-CRM-standard/internal/access/domain"
	accessport "github.com/qianlan33333-png/AI-CRM-standard/internal/access/port"
)

func TestCLIPlainIPPolicyNormalization(t *testing.T) {
	_, base, _ := authenticationFixture(t, 5)
	repo := &cliMemory{base, accessport.CLIPolicy{Version: 1}}
	service, err := NewAuthentication(repo, testUOW{}, testPasswords{}, AuthenticationConfig{DummyPHCHash: "hash:dummy"})
	if err != nil {
		t.Fatal(err)
	}
	user := repo.users[1]
	actor := domain.Principal{Kind: domain.KindAdmin, InternalID: 1, Roles: user.Roles, SessionVersion: user.SessionVersion}
	policy, err := service.SetCLIPolicy(context.Background(), actor, 1, []string{" 115.190.113.5 ", "115.190.113.5/32", "115.190.115.55", "2001:db8::1", "203.0.113.9/24"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"115.190.113.5/32", "115.190.115.55/32", "2001:db8::1/128", "203.0.113.0/24"}
	if !reflect.DeepEqual(policy.AllowedCIDRs, want) {
		t.Fatalf("policy=%v want=%v", policy.AllowedCIDRs, want)
	}
	if _, err = domain.NormalizeCIDRs([]string{"115.190.113.5"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("machine-client CIDR contract changed", err)
	}
	for _, bad := range []string{"115.190.113.999", "https://115.190.113.5", "fe80::1%en0", "203.0.113.5/99"} {
		prior := repo.policy.Version
		if _, err = service.SetCLIPolicy(context.Background(), actor, 1, []string{bad}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("accepted %s: %v", bad, err)
		}
		if repo.policy.Version != prior {
			t.Fatal("invalid IP changed persisted policy")
		}
	}
	if _, err = service.SetCLIPolicy(context.Background(), domain.Principal{Roles: []domain.Role{domain.RoleAdmin}}, 1, []string{"115.190.113.5"}); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatal("ordinary admin policy write", err)
	}
}
