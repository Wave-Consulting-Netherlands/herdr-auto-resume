package coordinator

import (
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

// Mode controls whether a Claude Code pane may be continued automatically.
type Mode int

const (
	ModeOff Mode = iota
	ModeAuto
)

func (m Mode) String() string {
	if m == ModeAuto {
		return "auto"
	}
	return "off"
}

type PaneState struct {
	Pane                 runtime.Pane
	Mode                 Mode
	UserDisabled         bool
	ProviderChecked      bool
	Provider             string
	HasClaudeCode        bool
	IsRateLimited        bool
	RateLimitResets      string
	RateLimitTime        time.Time
	ContinueSent         bool
	LastPeriodicContinue time.Time
}
