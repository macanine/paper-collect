package service

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/model"
)

// A node only receives credentials for drives its routing allows, and never
// for local drives, disabled drives or copies still in transit.
func TestNodeBlobSourcesFollowDriveRouting(t *testing.T) {
	svc, conn := newWorkerTestService(t, t.TempDir(), "master")
	ctx := context.Background()
	now := time.Now().UTC()
	sha := strings.Repeat("d", 64)
	blob := model.Blob{ID: newID(), SHA256: sha, SizeBytes: 1, MediaType: "application/pdf", FileName: "a.pdf", CreatedAt: now}
	if err := conn.Create(&blob).Error; err != nil {
		t.Fatal(err)
	}
	node := addWorkerNode(t, conn, false)
	other := addWorkerNode(t, conn, false)
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
	route := func(d model.Drive, n model.Node) {
		if err := conn.Create(&model.DriveNodeRoute{ID: newID(), DriveID: d.ID, NodeID: n.ID, Enabled: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	pool := drive("pool", model.KindS3, model.DriveRoutePool, true, 20, model.ReplicaReady)
	mine := drive("mine", model.KindWebDAV, model.DriveRouteExclusive, true, 10, model.ReplicaReady)
	route(mine, node)
	theirs := drive("theirs", model.KindWebDAV, model.DriveRouteExclusive, true, 5, model.ReplicaReady)
	route(theirs, other)
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
	want := []string{mine.ID + "=remote-mine", pool.ID + "=remote-pool"}
	if !slices.Equal(got, want) {
		t.Fatalf("sources = %v, want %v (routed drive first by read order)", got, want)
	}
}
