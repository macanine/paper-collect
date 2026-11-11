package service

import (
	"context"

	"github.com/nic/paper-collect/backend/internal/jobexec"
	"github.com/nic/paper-collect/backend/internal/model"
)

// nodeBlobSources lists the cloud replicas of an original that a worker node
// may read directly, so originals need not flow out through the master. A
// drive's routing decides which nodes may hold its credentials: pool drives
// are open to every node, preferred and exclusive drives only to the nodes
// routed to them. Local drives live on the master's disk and never qualify.
func (s *Service) nodeBlobSources(ctx context.Context, sha, nodeID string) ([]jobexec.BlobSource, error) {
	var blob model.Blob
	if err := s.db.WithContext(ctx).Select("id").Where("sha256 = ?", sha).Limit(1).Find(&blob).Error; err != nil || blob.ID == "" {
		return nil, err
	}
	sources, err := s.readyReplicaSources(ctx, blob.ID)
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	var routed []string
	if err := s.db.WithContext(ctx).Model(&model.DriveNodeRoute{}).
		Where("node_id = ? AND enabled = ?", nodeID, true).Pluck("drive_id", &routed).Error; err != nil {
		return nil, err
	}
	allowed := toSet(routed)
	out := []jobexec.BlobSource{}
	for _, source := range sources {
		drive, replica := source.provider, source.replica
		if !drive.Enabled || drive.Kind == model.KindLocal {
			continue
		}
		if replica.Status != model.ReplicaReady && replica.Status != model.ReplicaOrphaned {
			continue
		}
		if mode := drive.RouteMode; mode != "" && mode != model.DriveRoutePool && !allowed[drive.ID] {
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
