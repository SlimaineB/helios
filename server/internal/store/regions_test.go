package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/slim/helios/server/internal/model"
)

func TestRegionCatalogCRUDAndRenameCascade(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	region, err := st.CreateRegion(ctx, model.Region{Name: "eu-west-1", Description: "Paris"})
	if err != nil || region.ID == 0 {
		t.Fatalf("create region: %v %+v", err, region)
	}
	if _, err := st.CreateRegion(ctx, model.Region{Name: "EU-WEST-1"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("case-insensitive duplicate: want ErrConflict, got %v", err)
	}

	if _, err := st.CreateInstance(ctx, model.Instance{
		ID: "ins-1", Name: "one", TokenHash: "h1", Region: "eu-west-1",
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	regions, err := st.ListRegions(ctx)
	if err != nil || len(regions) != 1 || regions[0].Instances != 1 {
		t.Fatalf("list regions: %v %+v", err, regions)
	}

	region.Name = "eu-west-primary"
	region.Description = "Paris primary"
	if err := st.UpdateRegion(ctx, region); err != nil {
		t.Fatalf("rename region: %v", err)
	}
	inst, err := st.GetInstance(ctx, "ins-1")
	if err != nil || inst.Region != "eu-west-primary" {
		t.Fatalf("instance region after rename = %q err=%v", inst.Region, err)
	}

	got, err := st.GetRegionByID(ctx, region.ID)
	if err != nil || got.Name != "eu-west-primary" || got.Description != "Paris primary" || got.Instances != 1 {
		t.Fatalf("get region: %v %+v", err, got)
	}

	if err := st.DeleteRegion(ctx, region.ID); err != nil {
		t.Fatalf("delete region: %v", err)
	}
	if _, err := st.GetRegionByID(ctx, region.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted region lookup: want ErrNotFound, got %v", err)
	}
}
