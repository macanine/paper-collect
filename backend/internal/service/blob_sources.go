package service

import (
	"context"

	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
)

// nodeBlobSources lists the cloud replicas of an original that a worker node
// may read directly, so originals need not flow out through the master. Only
// drives the node may operate under driveRouteScope qualify, the same rule
// that routes uploads, so a node holds the credentials of exactly the drives
// it would upload to. Local drives live on the master's disk and never qualify.
func (s *Service) nodeBlobSources(ctx context.Context, sha, nodeID string) ([]jobexec.BlobSource, error) {
	var blob model.Blob
	if err := s.db.WithContext(ctx).Select("id").Where("sha256 = ?", sha).Limit(1).Find(&blob).Error; err != nil || blob.ID == "" {
		return nil, err
	}
	sources, err := s.readyReplicaSources(ctx, blob.ID)
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	managedIDs, allowedIDs, err := s.driveRouteScope(s.db.WithContext(ctx), nodeID)
	if err != nil {
		return nil, err
	}
	managed, allowed := toSet(managedIDs), toSet(allowedIDs)
	out := []jobexec.BlobSource{}
	for _, source := range sources {
		drive, replica := source.provider, source.replica
		if !drive.Enabled || drive.Kind == model.KindLocal {
			continue
		}
		if replica.Status != model.ReplicaReady && replica.Status != model.ReplicaOrphaned {
			continue
		}
		if managed[drive.ID] && !allowed[drive.ID] {
			continue
		}
		cfg, err := s.driveConfig(drive)
		if err != nil {
			continue // undecryptable credentials: the master path still works
		}
		remote := replica.RemoteID
		if remote == "" {
			remote = replica.RemotePath
		}
		out = append(out, jobexec.BlobSource{Drive: cfg, Remote: remote})
	}
	return out, nil
}
