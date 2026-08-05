package hec

import "fmt"

const (
	Version         = "0.0.1"
	ProtocolVersion = "HEC1/1.0.0"
)

var (
	BuildCommit = "unknown"
	BuildDate   = "unknown"
)

func VersionText() string {
	return fmt.Sprintf("HEC %s\nProtocol %s\nCommit %s\nBuilt %s", Version, ProtocolVersion, BuildCommit, BuildDate)
}
