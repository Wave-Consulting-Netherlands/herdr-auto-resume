package coordinator

import (
	"reflect"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

func TestSendResumeActionMatchesLegacyClaudeSequence(t *testing.T) {
	fake := &runtime.Fake{}
	var sleeps []time.Duration
	action := provider.ResumeAction{KeysBefore: []string{"escape"}, Text: "continue", SubmitKey: "enter"}
	if err := SendResumeAction(fake, "p1", action, func(d time.Duration) { sleeps = append(sleeps, d) }); err != nil {
		t.Fatal(err)
	}
	if want := []string{"SendKeys(p1,escape)", "SendText(p1,continue)", "SendKeys(p1,enter)"}; !reflect.DeepEqual(fake.Calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.Calls, want)
	}
	if want := []time.Duration{100 * time.Millisecond}; !reflect.DeepEqual(sleeps, want) {
		t.Fatalf("sleeps = %#v, want %#v", sleeps, want)
	}
}
