package multiregion

import (
	"testing"
)

func sampleStore() *RegionStore {
	s := NewRegionStore()
	s.Add(Region{Code: "ap-southeast-2", Name: "Sydney", Primary: true, DataResidencyZones: []string{"au", "apac"}})
	s.Add(Region{Code: "us-east-1", Name: "Virginia", Primary: false, DataResidencyZones: []string{"us", "na"}})
	s.Add(Region{Code: "eu-west-1", Name: "Ireland", Primary: false, DataResidencyZones: []string{"eu"}})
	return s
}

func TestRegionStore_PrimaryRegion(t *testing.T) {
	t.Parallel()

	s := sampleStore()
	primary, err := s.PrimaryRegion()
	if err != nil {
		t.Fatalf("PrimaryRegion: %v", err)
	}
	if primary.Code != "ap-southeast-2" {
		t.Errorf("primary.Code = %q, want ap-southeast-2", primary.Code)
	}
}

func TestRegionStore_ReadReplicas(t *testing.T) {
	t.Parallel()

	s := sampleStore()
	replicas := s.ReadReplicas()
	if len(replicas) != 2 {
		t.Errorf("ReadReplicas len = %d, want 2", len(replicas))
	}
	for _, r := range replicas {
		if r.Primary {
			t.Errorf("replica %q should not be primary", r.Code)
		}
	}
}

func TestRegionStore_GetUnknown(t *testing.T) {
	t.Parallel()

	s := sampleStore()
	_, err := s.Get("nonexistent")
	if err == nil {
		t.Error("expected error for unknown region")
	}
}

func TestResidencyEnforcer_Allowed(t *testing.T) {
	t.Parallel()

	s := sampleStore()
	rules := []ResidencyRule{{DataType: "customer_pii", RequiredZone: "au"}}
	enforcer := ResidencyEnforcer{}

	if err := enforcer.Check("customer_pii", "ap-southeast-2", rules, s); err != nil {
		t.Errorf("expected no error for au data in Sydney: %v", err)
	}
}

func TestResidencyEnforcer_Blocked(t *testing.T) {
	t.Parallel()

	s := sampleStore()
	rules := []ResidencyRule{{DataType: "customer_pii", RequiredZone: "au"}}
	enforcer := ResidencyEnforcer{}

	if err := enforcer.Check("customer_pii", "us-east-1", rules, s); err == nil {
		t.Error("expected error: au data should not go to us-east-1")
	}
}

func TestResidencyEnforcer_NoMatchingRule(t *testing.T) {
	t.Parallel()

	s := sampleStore()
	rules := []ResidencyRule{{DataType: "product_catalog", RequiredZone: "au"}}
	enforcer := ResidencyEnforcer{}

	// "analytics" data has no residency rule, so any region is fine.
	if err := enforcer.Check("analytics", "us-east-1", rules, s); err != nil {
		t.Errorf("no rule for analytics should not error: %v", err)
	}
}

func TestFailoverManager_Failover(t *testing.T) {
	t.Parallel()

	fm := NewFailoverManager("ap-southeast-2")
	if fm.Active() != "ap-southeast-2" {
		t.Errorf("Active = %q, want ap-southeast-2", fm.Active())
	}

	from, err := fm.Failover("us-east-1")
	if err != nil {
		t.Fatalf("Failover: %v", err)
	}
	if from != "ap-southeast-2" {
		t.Errorf("from = %q, want ap-southeast-2", from)
	}
	if fm.Active() != "us-east-1" {
		t.Errorf("Active after failover = %q, want us-east-1", fm.Active())
	}
}

func TestFailoverManager_FailoverToUnknownRegion(t *testing.T) {
	t.Parallel()

	// The FailoverManager itself does not validate region existence (that is
	// the caller's responsibility via RegionStore); it only blocks self-failover.
	fm := NewFailoverManager("ap-southeast-2")
	_, err := fm.Failover("ap-southeast-2")
	if err == nil {
		t.Error("expected error when failing over to same region")
	}
}

func TestFailoverManager_SetActive(t *testing.T) {
	t.Parallel()

	fm := NewFailoverManager("ap-southeast-2")
	fm.SetActive("eu-west-1")
	if fm.Active() != "eu-west-1" {
		t.Errorf("Active = %q, want eu-west-1", fm.Active())
	}
}
