// Package domain holds Aptify's pure core: identifiers, errors, the target-stack check and
// path safety. Nothing in it performs I/O, reads the clock or draws randomness.
package domain

import (
	"encoding/hex"
	"strconv"
	"strings"
)

// Distinct id types, so one kind of id cannot be passed where another is expected.
type (
	ProjectID        string
	PromptRevisionID string
	SpecificationID  string
	RunID            string
	ArtifactSetID    string
	UserID           string
	WorkspaceID      string
	// ContentHash is the hex SHA-256 of a file's content.
	ContentHash string
)

// Prefixes for generated ids.
const (
	PrefixProject        = "proj"
	PrefixPromptRevision = "pr"
	PrefixSpecification  = "spec"
	PrefixRun            = "run"
	PrefixArtifactSet    = "as"
)

const timeWidth = 9

// GenerateID returns "<prefix>_<time base36, 9 chars><16 random hex>". The time prefix keeps
// ids sortable by creation time, which keeps database indexes append-friendly. The caller
// supplies the clock and the randomness.
func GenerateID(prefix string, nowMs int64, random [8]byte) string {
	t := strconv.FormatInt(nowMs, 36)
	if len(t) < timeWidth {
		t = strings.Repeat("0", timeWidth-len(t)) + t
	}
	return prefix + "_" + t + hex.EncodeToString(random[:])
}
