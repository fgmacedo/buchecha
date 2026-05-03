package director

import (
	"crypto/sha256"
	"encoding/hex"
)

// SpecHash returns the canonical hash of a spec file's bytes. The hash
// is computed in a single pass over the raw bytes (no normalization,
// no formatting) so cosmetic transformations of the same content
// produce different hashes by design: the loop must replan when bytes
// change, even if the rendered Markdown is equivalent.
func SpecHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// PhaseID derives a stable identifier for a phase from the spec hash
// and the phase's intent. The same (specHash, intent) pair always
// produces the same ID; differing inputs almost certainly produce
// different IDs (sha256 collision space). The result is hex-encoded
// and truncated to 16 characters: short enough for filenames and TUI
// columns, long enough that practical collisions are not a concern at
// the cardinality of phases per spec.
//
// Stability across replans is the contract: when the user edits a
// spec but the Director keeps the same intent for a phase, the new
// plan reuses the prior ID and any DAG state collected against it
// remain addressable.
func PhaseID(specHash, intent string) string {
	h := sha256.New()
	h.Write([]byte(specHash))
	h.Write([]byte{0})
	h.Write([]byte(intent))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:16]
}

// TaskID derives a phase-scoped identifier for a task from the spec
// hash, the owning phase id, and the task's intent. Task ids are
// unique within their phase, not globally; the same (specHash,
// phaseID, intent) tuple is what stabilizes the id across replans.
// The result is hex-encoded and truncated to 16 characters.
func TaskID(specHash, phaseID, intent string) string {
	h := sha256.New()
	h.Write([]byte(specHash))
	h.Write([]byte{0})
	h.Write([]byte(phaseID))
	h.Write([]byte{0})
	h.Write([]byte(intent))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:16]
}
