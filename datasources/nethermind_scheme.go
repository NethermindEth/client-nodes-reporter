package datasources

import (
	"log/slog"
	"strconv"
	"strings"
	"time"
)

type StateSchemeBucket string

const (
	StateSchemeBucketPreV2      StateSchemeBucket = "pre-v2"
	StateSchemeBucketV2Halfpath StateSchemeBucket = "v2-halfpath"
	StateSchemeBucketV2Flat     StateSchemeBucket = "v2-flat"
	StateSchemeBucketV2Other    StateSchemeBucket = "v2-other"
)

type StateSchemeSnapshot struct {
	Network        string
	Total          int64
	PreV2          int64
	V2Halfpath     int64
	V2Flat         int64
	V2Other        int64
	Stale          int64
	NetworkELTotal int64
	Methodology    string
	SnapshotAt     time.Time
	CreatedAt      time.Time
}

// ObservedAt is when enrscout generated the data, falling back to when the row was stored.
func (s StateSchemeSnapshot) ObservedAt() time.Time {
	if !s.SnapshotAt.IsZero() {
		return s.SnapshotAt
	}
	return s.CreatedAt
}

func (s StateSchemeSnapshot) Compare(other StateSchemeSnapshot) int {
	return s.ObservedAt().Compare(other.ObservedAt())
}

func (s StateSchemeSnapshot) V2Total() int64 {
	return s.V2Halfpath + s.V2Flat + s.V2Other
}

// ClassifyNethermindVersion buckets a Nethermind client version string.
// From v2 onwards the state scheme is appended as the last dash-separated
// segment ("-hp" halfpath, "-f" flat), e.g. "v2.0.0+bec830cd-hp".
// Versions whose major cannot be parsed are treated as pre-v2.
func ClassifyNethermindVersion(version string) StateSchemeBucket {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")

	majorStr, _, _ := strings.Cut(v, ".")
	major, err := strconv.Atoi(majorStr)
	if err != nil || major < 2 {
		return StateSchemeBucketPreV2
	}

	idx := strings.LastIndex(v, "-")
	if idx < 0 {
		return StateSchemeBucketV2Other
	}

	switch v[idx+1:] {
	case "hp":
		return StateSchemeBucketV2Halfpath
	case "f":
		return StateSchemeBucketV2Flat
	default:
		return StateSchemeBucketV2Other
	}
}

func BuildStateSchemeSnapshot(network string, nodes []EnrScoutNode, stats EnrScoutStats, meta EnrScoutMeta) StateSchemeSnapshot {
	snapshot := StateSchemeSnapshot{
		Network:        network,
		NetworkELTotal: stats.Execution,
		Methodology:    meta.MethodologyVersion,
		SnapshotAt:     stats.SnapshotGeneratedAt,
		CreatedAt:      time.Now(),
	}

	otherVersions := make(map[string]int)
	for _, node := range nodes {
		snapshot.Total++
		if node.FPStatus != "ok" {
			snapshot.Stale++
		}

		switch ClassifyNethermindVersion(node.ClientVersion) {
		case StateSchemeBucketPreV2:
			snapshot.PreV2++
		case StateSchemeBucketV2Halfpath:
			snapshot.V2Halfpath++
		case StateSchemeBucketV2Flat:
			snapshot.V2Flat++
		case StateSchemeBucketV2Other:
			snapshot.V2Other++
			otherVersions[node.ClientVersion]++
		}
	}

	for version, count := range otherVersions {
		slog.Debug("Unrecognized v2 state scheme suffix", "version", version, "count", count)
	}

	return snapshot
}
