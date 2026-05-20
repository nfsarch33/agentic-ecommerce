package compliance_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/compliance"
)

func TestGDPR_DataMapListsLocations(t *testing.T) {
	t.Parallel()
	m := compliance.NewGDPRManager()
	m.AddUserData("U1", "orders", []byte(`{"orders":[1,2,3]}`))
	m.AddUserData("U1", "profiles", []byte(`{"name":"Alice"}`))
	inv, err := m.DataMap(nil, "U1")
	if err != nil {
		t.Fatalf("data map failed: %v", err)
	}
	if len(inv.Locations) < 2 {
		t.Fatalf("expected 2 data locations, got %d", len(inv.Locations))
	}
}

func TestGDPR_ExportBundlesAllData(t *testing.T) {
	t.Parallel()
	m := compliance.NewGDPRManager()
	m.AddUserData("U2", "orders", []byte("order-data"))
	m.AddUserData("U2", "reviews", []byte("review-data"))
	data, err := m.ExportUserData(nil, "U2")
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if len(data) < 2 {
		t.Fatalf("expected 2 systems in export, got %d", len(data))
	}
}

func TestGDPR_DeleteRemovesAndReturnsReceipt(t *testing.T) {
	t.Parallel()
	m := compliance.NewGDPRManager()
	m.AddUserData("U3", "profiles", []byte("pii"))
	receipt, err := m.DeleteUserData(nil, "U3")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if receipt.UserID != "U3" {
		t.Fatalf("expected receipt for U3, got %s", receipt.UserID)
	}
	if m.HasData("U3") {
		t.Fatal("expected data to be deleted")
	}
}

func TestGDPR_ConsentLogRecordsGrant(t *testing.T) {
	t.Parallel()
	m := compliance.NewGDPRManager()
	m.RecordConsent(nil, "U4", "marketing", true)
	consents := m.GetConsents("U4")
	if len(consents) != 1 || !consents[0].Granted {
		t.Fatal("expected granted consent recorded")
	}
}

func TestGDPR_ConsentLogRecordsRevoke(t *testing.T) {
	t.Parallel()
	m := compliance.NewGDPRManager()
	m.RecordConsent(nil, "U5", "analytics", true)
	m.RecordConsent(nil, "U5", "analytics", false)
	consents := m.GetConsents("U5")
	if len(consents) != 2 {
		t.Fatalf("expected 2 consent entries, got %d", len(consents))
	}
	if consents[1].Granted {
		t.Fatal("expected revocation to be recorded")
	}
}

func TestGDPR_DeleteRespectsRetentionHold(t *testing.T) {
	t.Parallel()
	m := compliance.NewGDPRManager()
	m.AddUserData("U6", "billing", []byte("billing-data"))
	m.SetRetentionHold("U6", true)
	_, err := m.DeleteUserData(nil, "U6")
	if err != compliance.ErrRetentionHold {
		t.Fatalf("expected ErrRetentionHold, got %v", err)
	}
}

func TestGDPR_ExportNonExistentUserError(t *testing.T) {
	t.Parallel()
	m := compliance.NewGDPRManager()
	_, err := m.ExportUserData(nil, "ghost")
	if err != compliance.ErrGDPRUserNotFound {
		t.Fatalf("expected ErrGDPRUserNotFound, got %v", err)
	}
}
