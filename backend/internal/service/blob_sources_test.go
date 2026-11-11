package service

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
)

// A node reads (and receives credentials for) exactly the drives it would
// upload to, and never local drives, disabled drives or copies in transit.
func TestNodeBlobSourcesFollowDriveRouting(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	ctx := context.Background()
	now := time.Now().UTC()
	sha := strings.Repeat("d", 64)
	blob := model.Blob{ID: newID(), SHA256: sha, SizeBytes: 1, MediaType: "application/pdf", FileName: "a.pdf", CreatedAt: now}
	if err := conn.Create(&blob).Error; err != nil {
		t.Fatal(err)
	}
	node := addLiveNode(t, conn, newID(), "本节点", false)
	other := addLiveNode(t, conn, newID(), "其他节点", false)
	offline := addWorkerNode(t, conn, false) // never seen: unavailable
	drive := func(name, kind, mode string, enabled bool, order int, status string) model.Drive {
		d := model.Drive{ID: newID(), Name: name, Kind: kind, Enabled: true, RouteMode: mode, ReadOrder: order,
			AllowedExtsJSON: "[]", OnPathChange: model.OnPathChangeMove, ConfigJSON: "{}", CreatedAt: now, UpdatedAt: now}
		if err := conn.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
		if !enabled {
			conn.Model(&model.Drive{}).Where("id = ?", d.ID).Update("enabled", false)
		}
		r := model.Replica{ID: newID(), BlobID: blob.ID, DriveID: d.ID, Status: status, RemoteID: "remote-" + name, CreatedAt: now, UpdatedAt: now}
		if err := conn.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		return d
	}
	route := func(d model.Drive, n model.Node, priority ...int) {
		p := 0
		if len(priority) > 0 {
			p = priority[0]
		}
		if err := conn.Create(&model.DriveNodeRoute{ID: newID(), DriveID: d.ID, NodeID: n.ID, Priority: p, Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	pool := drive("pool", model.KindS3, model.DriveRoutePool, true, 20, model.ReplicaReady)
	mine := drive("mine", model.KindWebDAV, model.DriveRouteExclusive, true, 10, model.ReplicaReady)
	route(mine, node)
	theirs := drive("theirs", model.KindWebDAV, model.DriveRouteExclusive, true, 5, model.ReplicaReady)
	route(theirs, other)
	// Preferred drive whose routed node is offline: fails over to every node.
	failover := drive("failover", model.KindWebDAV, model.DriveRoutePreferred, true, 15, model.ReplicaReady)
	route(failover, offline)
	// Preferred drive where this node is only the backup of a live primary.
	backup := drive("backup", model.KindWebDAV, model.DriveRoutePreferred, true, 4, model.ReplicaReady)
	route(backup, other, 0)
	route(backup, node, 1)
	// Exclusive drive whose only node is offline: waits, never falls over.
	waiting := drive("waiting", model.KindWebDAV, model.DriveRouteExclusive, true, 6, model.ReplicaReady)
	route(waiting, offline)
	drive("local", model.KindLocal, model.DriveRoutePool, true, 1, model.ReplicaReady)
	drive("off", model.KindS3, model.DriveRoutePool, false, 2, model.ReplicaReady)
	drive("moving", model.KindS3, model.DriveRoutePool, true, 3, model.ReplicaMoving)

	sources, err := svc.nodeBlobSources(ctx, sha, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range sources {
		got = append(got, s.Drive.ID+"="+s.Remote)
	}
	want := []string{mine.ID + "=remote-mine", failover.ID + "=remote-failover", pool.ID + "=remote-pool"}
	if !slices.Equal(got, want) {
		t.Fatalf("sources = %v, want %v (in read order)", got, want)
	}

	// The same rule routes uploads: the claim scope agrees drive by drive.
	scope, err := svc.applyRouteScope(conn, claimer{ID: node.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []model.Drive{mine, failover} {
		if !slices.Contains(scope.RoutedAllowedIDs, d.ID) {
			t.Errorf("upload scope misses readable drive %s", d.Name)
		}
	}
	for _, d := range []model.Drive{theirs, backup, waiting} {
		if slices.Contains(scope.RoutedAllowedIDs, d.ID) {
			t.Errorf("upload scope allows unreadable drive %s", d.Name)
		}
	}
}
