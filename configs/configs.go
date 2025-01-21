package configs

import "strings"

// Context Keys
type ContextKey string

const (
	ContextKeyDebug    ContextKey = "debug"
	ContextKeyLogger   ContextKey = "logger"
	ContextKeySource   ContextKey = "source"
	ContextKeyDB       ContextKey = "database"
	ContextKeyNotifier ContextKey = "notifier"
)

// Clients
type ClientType string

const (
	ClientTypeNethermind ClientType = "nethermind"
	ClientTypeGeth       ClientType = "geth"
	ClientTypeBesu       ClientType = "besu"
	ClientTypeErigon     ClientType = "erigon"
	ClientTypeReth       ClientType = "reth"
	ClientTypeUnknown    ClientType = "unknown"
)

func ClientTypeFromString(s string) ClientType {
	switch strings.ToLower(s) {
	case "nethermind":
		return ClientTypeNethermind
	case "geth":
		return ClientTypeGeth
	case "besu":
		return ClientTypeBesu
	case "erigon":
		return ClientTypeErigon
	case "reth":
		return ClientTypeReth
	default:
		return ClientTypeUnknown
	}
}

func (c ClientType) String() string {
	switch c {
	case ClientTypeNethermind:
		return "Nethermind"
	case ClientTypeGeth:
		return "Geth"
	case ClientTypeBesu:
		return "Besu"
	case ClientTypeErigon:
		return "Erigon"
	case ClientTypeReth:
		return "Reth"
	default:
		return "Unknown"
	}
}
