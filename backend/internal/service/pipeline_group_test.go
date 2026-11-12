package service

import (
	"testing"
	"time"

	"github.com/nic/paper-collect/backend/internal/catalogai"
	"github.com/nic/paper-collect/backend/internal/model"
)

// One file per upload makes every file its own batch, so grouping must still
// see the other files that arrived in the same folder.
func TestGroupPeersPullsSameFolderFilesFromOtherUploads(t *testing.T) {
	e := newSyncEnv(t)
	questions := e.material("试题", "试题.pdf", []byte("peer-questions"), nil)
	answers := e.material("答案", "答案.pdf", []byte("peer-answers"), nil)
	elsewhere := e.material("别处", "别处.pdf", []byte("peer-elsewhere"), nil)

	now := time.Now().UTC()
	place := func(m model.Material, batch, arrivedAt string) {
		if err := e.conn.Model(&model.Material{}).Where("id = ?", m.ID).Updates(map[string]any{
			"batch_id": batch, "ai_step": model.StepGroup, "stage": model.StageProcessing, "updated_at": now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := e.conn.Create(&model.MaterialSource{ID: newID(), MaterialID: m.ID, Path: arrivedAt, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	place(questions, "batch-a", "资料/试题.pdf")
	place(answers, "batch-b", "资料/答案.pdf")
	place(elsewhere, "batch-c", "其他/别处.pdf")

	rows, paths := e.svc.batchOrder(e.conn, []model.Material{questions})
	peers, err := e.svc.groupPeers(e.conn, "batch-a", rows, paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].ID != answers.ID {
		t.Fatalf("peers = %+v, want only the answers file", peers)
	}
}

// A set that already holds files from the folder is always offered as a
// candidate, so a late answer file can join the set its questions are in.
func TestFolderSetsArePinnedAsGroupCandidates(t *testing.T) {
	e := newSyncEnv(t)
	questions := e.material("试题", "试题.pdf", []byte("pin-questions"), nil)
	if err := e.conn.Create(&model.MaterialSource{ID: newID(), MaterialID: questions.ID,
		Path: "资料/试题.pdf", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}

	pinned := map[string]bool{}
	files := []catalogai.GroupFile{{ID: "late", Path: "资料/答案.pdf"}}
	if err := pinFolderPacks(e.conn, files, pinned); err != nil {
		t.Fatal(err)
	}
	if !pinned[questions.PackID] {
		t.Fatalf("set of the same folder was not pinned: %v", pinned)
	}

	// A top-level file has no folder to share, so nothing is pinned.
	pinned = map[string]bool{}
	if err := pinFolderPacks(e.conn, []catalogai.GroupFile{{ID: "root", Path: "答案.pdf"}}, pinned); err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 0 {
		t.Fatalf("a top-level file pinned %v", pinned)
	}
}
