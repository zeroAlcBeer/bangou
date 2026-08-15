package committed

import (
	"context"
	"database/sql"
	"testing"
)

func testStore(t *testing.T) Store {
	t.Helper()
	s, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPipelineCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr", PathPattern: "{Year}/{Number}", ScrapeProviders: "dmm,avwiki"})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	p, err := s.GetPipeline(ctx, id)
	if err != nil || p == nil {
		t.Fatalf("get: %v", err)
	}
	if p.Name != "VR" || p.InputDir != "/dl/vr" {
		t.Fatalf("unexpected: %+v", p)
	}

	list, err := s.ListPipelines(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}

	// Unique input_dir constraint
	_, err = s.CreatePipeline(ctx, &Pipeline{Name: "VR2", InputDir: "/dl/vr", OutputDir: "/media/vr2"})
	if err == nil {
		t.Fatal("expected unique constraint error")
	}

	if err := s.DeletePipeline(ctx, id); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListPipelines(ctx)
	if len(list) != 0 {
		t.Fatal("expected empty")
	}
}

func TestProviderConfig(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	cfg, err := s.GetProviderConfig(ctx, "dmm")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != "{}" {
		t.Fatalf("expected empty config, got %q", cfg)
	}

	if err := s.SetProviderConfig(ctx, "dmm", `{"api_id":"x"}`); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.GetProviderConfig(ctx, "dmm")
	if cfg != `{"api_id":"x"}` {
		t.Fatalf("got %q", cfg)
	}

	list, err := s.ListProviderConfigs(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
}

func TestBangouAndFiles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pid, _ := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr"})

	bid, err := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "ACHJ-057", OutDir: "/media/vr/ACHJ-057"})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.CreateBangouFile(ctx, &BangouFile{BangouID: bid, LinkPath: "/media/vr/ACHJ-057/ACHJ-057.mp4", LinkType: "hardlink"}); err != nil {
		t.Fatal(err)
	}

	ok, err := s.IsBangouCommitted(ctx, pid, "ACHJ-057")
	if err != nil || !ok {
		t.Fatalf("is committed: %v %v", ok, err)
	}

	bangous, total, err := s.ListBangousByPipeline(ctx, pid, 10, 0, "added", "desc", "", "")
	if err != nil || total != 1 || len(bangous) != 1 {
		t.Fatalf("bangous: %v total=%d len=%d", err, total, len(bangous))
	}
	if bangous[0].Number != "ACHJ-057" {
		t.Fatalf("unexpected bangou: %+v", bangous[0])
	}

	files, err := s.ListBangouFilesByBangou(ctx, bid)
	if err != nil || len(files) != 1 {
		t.Fatalf("files: %v len=%d", err, len(files))
	}
}

func TestGetBangouFileByID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pid, _ := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr"})
	bid, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "ACHJ-057", OutDir: "/media/vr/ACHJ-057"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: bid, LinkPath: "/out/a.mp4", LinkType: "hardlink"})

	files, _ := s.ListBangouFilesByBangou(ctx, bid)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	got, err := s.GetBangouFileByID(ctx, files[0].ID)
	if err != nil {
		t.Fatalf("get file by id: %v", err)
	}
	if got.LinkPath != "/out/a.mp4" {
		t.Fatalf("unexpected file: %+v", got)
	}

	if _, err := s.GetBangouFileByID(ctx, 999999); err == nil {
		t.Fatal("expected sql.ErrNoRows")
	} else if err != sql.ErrNoRows {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestBangouMultiPart(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pid, _ := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr"})

	bid1, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "SIVR-296", OutDir: "/out/SIVR-296"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: bid1, LinkPath: "/out/SIVR-296/SIVR-296-cd1.mp4", LinkType: "hardlink"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: bid1, LinkPath: "/out/SIVR-296/SIVR-296-cd2.mp4", LinkType: "hardlink"})

	bid2, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "ACHJ-057", OutDir: "/out/ACHJ-057"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: bid2, LinkPath: "/out/ACHJ-057/ACHJ-057.mp4", LinkType: "hardlink"})

	bangous, total, err := s.ListBangousByPipeline(ctx, pid, 10, 0, "number", "asc", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(bangous) != 2 {
		t.Fatalf("total=%d len=%d", total, len(bangous))
	}
	if bangous[0].Number != "ACHJ-057" {
		t.Fatalf("bangou[0] = %+v", bangous[0])
	}
	if bangous[1].Number != "SIVR-296" {
		t.Fatalf("bangou[1] = %+v", bangous[1])
	}

	files, _ := s.ListBangouFilesByBangou(ctx, bid1)
	if len(files) != 2 {
		t.Fatalf("expected 2 files for SIVR-296, got %d", len(files))
	}
}

func TestListBangousByPipelineStatus(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pid, _ := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr"})

	aliveID, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "ALIVE-001", OutDir: "/out/alive"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: aliveID, LinkPath: "/out/alive/a.mp4", LinkType: "hardlink"})

	missingID, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "MISS-001", OutDir: "/out/missing"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: missingID, LinkPath: "/out/missing/a.mp4", LinkType: "hardlink"})
	missingFiles, _ := s.ListBangouFilesByBangou(ctx, missingID)
	_ = s.SetBangouFileAlive(ctx, missingFiles[0].ID, false)

	multiID, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "MULTI-001", OutDir: "/out/multi"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: multiID, LinkPath: "/out/multi/a-cd1.mp4", LinkType: "hardlink"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: multiID, LinkPath: "/out/multi/a-cd2.mp4", LinkType: "hardlink"})
	multiFiles, _ := s.ListBangouFilesByBangou(ctx, multiID)
	_ = s.SetBangouFileAlive(ctx, multiFiles[1].ID, false)

	_, total, err := s.ListBangousByPipeline(ctx, pid, 10, 0, "number", "asc", "", "")
	if err != nil || total != 3 {
		t.Fatalf("all total=%d err=%v", total, err)
	}

	alive, total, err := s.ListBangousByPipeline(ctx, pid, 10, 0, "number", "asc", "alive", "")
	if err != nil || total != 1 || len(alive) != 1 || alive[0].Number != "ALIVE-001" {
		t.Fatalf("alive total=%d items=%+v err=%v", total, alive, err)
	}

	missing, total, err := s.ListBangousByPipeline(ctx, pid, 10, 0, "number", "asc", "missing", "")
	if err != nil || total != 2 || len(missing) != 2 {
		t.Fatalf("missing total=%d items=%+v err=%v", total, missing, err)
	}
	if missing[0].Number != "MISS-001" || missing[1].Number != "MULTI-001" {
		t.Fatalf("unexpected missing order/items: %+v", missing)
	}
}

func TestMetadataWithBangou(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pid, _ := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr"})
	bid, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "ACHJ-057", OutDir: "/out/ACHJ-057"})

	if err := s.UpsertMetadata(ctx, &Metadata{BangouID: bid, Number: "ACHJ-057", Title: "Test Title"}); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMetadataByBangou(ctx, bid)
	if err != nil || m == nil || m.Title != "Test Title" {
		t.Fatalf("metadata: %+v err=%v", m, err)
	}

	// Upsert should update
	if err := s.UpsertMetadata(ctx, &Metadata{BangouID: bid, Number: "ACHJ-057", Title: "Updated"}); err != nil {
		t.Fatal(err)
	}
	m, _ = s.GetMetadataByBangou(ctx, bid)
	if m.Title != "Updated" {
		t.Fatalf("expected updated title, got %q", m.Title)
	}
}

func TestSettingsLegacy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.SetSetting(ctx, "input_dir", "/input"); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetSetting(ctx, "input_dir")
	if err != nil || v != "/input" {
		t.Fatalf("setting: %q err=%v", v, err)
	}
}

func TestDeleteBangouCascades(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pid, _ := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr"})
	bid, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "ACHJ-057", OutDir: "/out/ACHJ-057"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: bid, LinkPath: "/out/a.mp4", LinkType: "hardlink"})
	_ = s.UpsertMetadata(ctx, &Metadata{BangouID: bid, Number: "ACHJ-057", Title: "T"})

	if err := s.DeleteBangou(ctx, bid); err != nil {
		t.Fatal(err)
	}

	files, _ := s.ListBangouFilesByBangou(ctx, bid)
	if len(files) != 0 {
		t.Fatal("expected files deleted")
	}
	m, _ := s.GetMetadataByBangou(ctx, bid)
	if m != nil {
		t.Fatal("expected metadata deleted")
	}
	b, _ := s.GetBangou(ctx, bid)
	if b != nil {
		t.Fatal("expected bangou deleted")
	}
}

func TestBangouUniqueConstraint(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pid, _ := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr"})
	_, err := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "ACHJ-057", OutDir: "/out/ACHJ-057"})
	if err != nil {
		t.Fatal(err)
	}
	// Same pipeline + number should fail
	_, err = s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "ACHJ-057", OutDir: "/out/ACHJ-057-2"})
	if err == nil {
		t.Fatal("expected unique constraint error")
	}
}

func TestCountBangouFiles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pid, _ := s.CreatePipeline(ctx, &Pipeline{Name: "VR", InputDir: "/dl/vr", OutputDir: "/media/vr"})
	bid, _ := s.CreateBangou(ctx, &Bangou{PipelineID: pid, Number: "SIVR-296", OutDir: "/out/SIVR-296"})

	count, _ := s.CountBangouFiles(ctx, bid)
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: bid, LinkPath: "/out/cd1.mp4", LinkType: "hardlink"})
	_ = s.CreateBangouFile(ctx, &BangouFile{BangouID: bid, LinkPath: "/out/cd2.mp4", LinkType: "hardlink"})
	count, _ = s.CountBangouFiles(ctx, bid)
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}
}
