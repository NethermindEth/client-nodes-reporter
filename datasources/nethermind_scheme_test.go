package datasources

import "testing"

func TestClassifyNethermindVersion(t *testing.T) {
	tests := []struct {
		version string
		want    StateSchemeBucket
	}{
		{"v1.39.3+28cbe2a0", StateSchemeBucketPreV2},
		{"v1.36.0", StateSchemeBucketPreV2},
		{"", StateSchemeBucketPreV2},
		{"garbage", StateSchemeBucketPreV2},
		{"v2.0.0+bec830cd-hp", StateSchemeBucketV2Halfpath},
		{"v2.0.0-rc2+4117f29b-hp", StateSchemeBucketV2Halfpath},
		{"2.0.0+bec830cd-hp", StateSchemeBucketV2Halfpath},
		{"v2.0.0+bec830cd-f", StateSchemeBucketV2Flat},
		{"v2.0.0-f", StateSchemeBucketV2Flat},
		{"v2.1.0-unstable+2bf95a3f-f", StateSchemeBucketV2Flat},
		{"v10.0.0+abcdef01-f", StateSchemeBucketV2Flat},
		{"v2.0.0+bec830cd-hpA", StateSchemeBucketV2Other},
		{"v2.0.0+bec830cd-h", StateSchemeBucketV2Other},
		{"v2.0.0+bec830cd-fit", StateSchemeBucketV2Other},
		{"v2.1.0-unstable+ae016030-pf", StateSchemeBucketV2Other},
		{"v2.0.0-rc2", StateSchemeBucketV2Other},
		{"v2.0.0", StateSchemeBucketV2Other},
	}

	for _, tt := range tests {
		if got := ClassifyNethermindVersion(tt.version); got != tt.want {
			t.Errorf("ClassifyNethermindVersion(%q) = %q, want %q", tt.version, got, tt.want)
		}
	}
}

func TestBuildStateSchemeSnapshot(t *testing.T) {
	nodes := []EnrScoutNode{
		{ID: "1", ClientVersion: "v1.39.3+28cbe2a0", FPStatus: "ok"},
		{ID: "2", ClientVersion: "v1.36.0+31cb81b7", FPStatus: "stale"},
		{ID: "3", ClientVersion: "v2.0.0+bec830cd-hp", FPStatus: "ok"},
		{ID: "4", ClientVersion: "v2.0.0+bec830cd-hp", FPStatus: "ok"},
		{ID: "5", ClientVersion: "v2.0.0+bec830cd-f", FPStatus: "failed"},
		{ID: "6", ClientVersion: "v2.0.0+bec830cd-fit", FPStatus: "ok"},
	}
	stats := EnrScoutStats{Execution: 100}
	meta := EnrScoutMeta{MethodologyVersion: "2026-08-v3"}

	got := BuildStateSchemeSnapshot("mainnet", nodes, stats, meta)

	if got.Network != "mainnet" || got.Methodology != "2026-08-v3" || got.NetworkELTotal != 100 {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.Total != 6 || got.PreV2 != 2 || got.V2Halfpath != 2 || got.V2Flat != 1 || got.V2Other != 1 {
		t.Errorf("unexpected buckets: %+v", got)
	}
	if got.Stale != 2 {
		t.Errorf("Stale = %d, want 2", got.Stale)
	}
	if got.V2Total() != 4 {
		t.Errorf("V2Total() = %d, want 4", got.V2Total())
	}
}
