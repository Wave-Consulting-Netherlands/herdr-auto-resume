package coordinator

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type failStepRuntime struct {
	runtime.Fake
	failAt int
	step   int
}

func (r *failStepRuntime) SendKeys(paneID string, keys ...string) error {
	r.step++
	r.Calls = append(r.Calls, fmt.Sprintf("SendKeys(%s,%s)", paneID, strings.Join(keys, ",")))
	if r.step == r.failAt {
		return errors.New("injected send failure")
	}
	r.SentKeys = append(r.SentKeys, runtime.KeysCall{PaneID: paneID, Keys: append([]string(nil), keys...)})
	return nil
}

func (r *failStepRuntime) SendText(paneID, text string) error {
	r.step++
	r.Calls = append(r.Calls, fmt.Sprintf("SendText(%s,%s)", paneID, text))
	if r.step == r.failAt {
		return errors.New("injected send failure")
	}
	r.SentText = append(r.SentText, runtime.TextCall{PaneID: paneID, Text: text})
	return nil
}

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

func TestSendResumeActionStopsAtFirstFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failAt    int
		wantCalls []string
	}{
		{name: "escape", failAt: 1, wantCalls: []string{"SendKeys(p1,escape)"}},
		{name: "text", failAt: 2, wantCalls: []string{"SendKeys(p1,escape)", "SendText(p1,continue)"}},
		{name: "enter", failAt: 3, wantCalls: []string{"SendKeys(p1,escape)", "SendText(p1,continue)", "SendKeys(p1,enter)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &failStepRuntime{failAt: tc.failAt}
			if err := SendResumeAction(fake, "p1", provider.ResumeAction{KeysBefore: []string{"escape"}, Text: "continue", SubmitKey: "enter"}, func(time.Duration) {}); err == nil {
				t.Fatal("SendResumeAction() error = nil, want injected failure")
			}
			if !reflect.DeepEqual(fake.Calls, tc.wantCalls) {
				t.Fatalf("calls = %#v, want %#v", fake.Calls, tc.wantCalls)
			}
		})
	}
}
