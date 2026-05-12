package marketplacesync

type MismatchReason string

const (
	MismatchMissingRemote MismatchReason = "missing_remote"
	MismatchVersion       MismatchReason = "version"
	MismatchRemoteOnly    MismatchReason = "remote_only"
)

type SnapshotItem struct {
	EntityID string
	Version  string
}

type ReconciliationMismatch struct {
	EntityID      string
	LocalVersion  string
	RemoteVersion string
	Reason        MismatchReason
}

type ReconciliationReport struct {
	TotalLocal  int
	TotalRemote int
	Mismatches  []ReconciliationMismatch
}

func Reconcile(local, remote []SnapshotItem) ReconciliationReport {
	report := ReconciliationReport{
		TotalLocal:  len(local),
		TotalRemote: len(remote),
	}
	remoteByID := make(map[string]SnapshotItem, len(remote))
	for _, item := range remote {
		remoteByID[item.EntityID] = item
	}
	seenLocal := make(map[string]struct{}, len(local))
	for _, item := range local {
		seenLocal[item.EntityID] = struct{}{}
		remoteItem, ok := remoteByID[item.EntityID]
		if !ok {
			report.Mismatches = append(report.Mismatches, ReconciliationMismatch{
				EntityID:     item.EntityID,
				LocalVersion: item.Version,
				Reason:       MismatchMissingRemote,
			})
			continue
		}
		if item.Version != remoteItem.Version {
			report.Mismatches = append(report.Mismatches, ReconciliationMismatch{
				EntityID:      item.EntityID,
				LocalVersion:  item.Version,
				RemoteVersion: remoteItem.Version,
				Reason:        MismatchVersion,
			})
		}
	}
	for _, item := range remote {
		if _, ok := seenLocal[item.EntityID]; ok {
			continue
		}
		report.Mismatches = append(report.Mismatches, ReconciliationMismatch{
			EntityID:      item.EntityID,
			RemoteVersion: item.Version,
			Reason:        MismatchRemoteOnly,
		})
	}
	return report
}
