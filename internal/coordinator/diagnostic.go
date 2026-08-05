package coordinator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

// logLimitedDiagnostic emits one compact record for each new evidence hash.
// The evidence itself is intentionally never written: terminal transcripts can
// contain sensitive content and are not needed to identify a repeated state.
func (c *Coordinator) logLimitedDiagnostic(pane runtime.Pane, providerName string, analysis detection.Analysis, content, reason string) {
	if c.logw == nil {
		return
	}
	evidence := analysis.Evidence
	if evidence == "" {
		evidence = content
	}
	digest := sha256.Sum256([]byte(evidence))
	evidenceHash := hex.EncodeToString(digest[:])
	if c.diagnosticEvidence[pane.ID] == evidenceHash {
		return
	}
	c.diagnosticEvidence[pane.ID] = evidenceHash
	if providerName == "" {
		providerName = "none"
	}
	fmt.Fprintf(c.logw, "limit diagnostic pane=%s provider=%s reason=%s reset=%q evidence=%s\n", pane.ID, providerName, reason, analysis.Reset.Raw, evidenceHash)
}

func (c *Coordinator) logTransientDiagnostic(pane runtime.Pane, providerName string, analysis detection.Analysis, content string) {
	if c.logw == nil {
		return
	}
	evidence := analysis.Evidence
	if evidence == "" {
		evidence = content
	}
	digest := sha256.Sum256([]byte(evidence))
	evidenceHash := hex.EncodeToString(digest[:])
	if c.transientEvidence[pane.ID] == evidenceHash {
		return
	}
	c.transientEvidence[pane.ID] = evidenceHash
	if providerName == "" {
		providerName = "none"
	}
	fmt.Fprintf(c.logw, "transient diagnostic pane=%s provider=%s reason=classified class=%s evidence=%s\n", pane.ID, providerName, analysis.TransientClass, evidenceHash)
}
