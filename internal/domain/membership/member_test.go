package membership

import (
	"errors"
	"testing"
)

func TestNewMemberValidates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   MemberInput
		wantErr error
	}{
		{name: "missing tenant", input: MemberInput{Email: "a@b.c"}, wantErr: ErrTenantRequired},
		{name: "missing email", input: MemberInput{TenantID: "tenant-a"}, wantErr: ErrMemberEmailRequired},
		{name: "invalid email", input: MemberInput{TenantID: "tenant-a", Email: "bogus"}, wantErr: ErrMemberEmailInvalid},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewMember(tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewMemberHappyPath(t *testing.T) {
	t.Parallel()

	member, err := NewMember(MemberInput{TenantID: " tenant-a ", Email: "  Alice@Example.COM  "})
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	if member.TenantID() != "tenant-a" {
		t.Fatalf("tenant = %q", member.TenantID())
	}
	if member.Email() != "alice@example.com" {
		t.Fatalf("email = %q", member.Email())
	}
	if member.JoinedAt().IsZero() || !member.UpdatedAt().Equal(member.JoinedAt()) {
		t.Fatalf("joinedAt/updatedAt mismatch: %v / %v", member.JoinedAt(), member.UpdatedAt())
	}
}

func TestReconstructMemberPreservesIdentity(t *testing.T) {
	t.Parallel()

	original, err := NewMember(MemberInput{TenantID: "tenant-a", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	rebuilt := ReconstructMember(MemberRecord{
		ID:        original.ID(),
		TenantID:  original.TenantID(),
		Email:     original.Email(),
		JoinedAt:  original.JoinedAt(),
		UpdatedAt: original.UpdatedAt(),
	})
	if rebuilt.ID() != original.ID() || rebuilt.Email() != original.Email() {
		t.Fatalf("reconstruct mismatch: %+v vs %+v", rebuilt, original)
	}
}
