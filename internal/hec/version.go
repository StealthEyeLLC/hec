package hec

import "fmt"

var (
	Version     = "0.0.1"
	BuildCommit = "unknown"
	BuildDate   = "unknown"
)

func VersionText() string {
	return fmt.Sprintf("HEC %s\nProtocol %s\nCommit %s\nBuilt %s", Version, ProtocolVersion, BuildCommit, BuildDate)
}
