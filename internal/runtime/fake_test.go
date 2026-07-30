package runtime

import (
	"reflect"
	"testing"
)

var _ Runtime = (*Fake)(nil)

func TestFakeRecordsTextAndKeys(t *testing.T) {
	fake := &Fake{}
	if err := fake.SendKeys("w1:p1", KeyEscape); err != nil {
		t.Fatalf("SendKeys returned error: %v", err)
	}
	if err := fake.SendText("w1:p1", "continue"); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}

	if !reflect.DeepEqual(fake.SentKeys, []KeysCall{{PaneID: "w1:p1", Keys: []string{KeyEscape}}}) {
		t.Fatalf("SentKeys = %#v", fake.SentKeys)
	}
	if !reflect.DeepEqual(fake.SentText, []TextCall{{PaneID: "w1:p1", Text: "continue"}}) {
		t.Fatalf("SentText = %#v", fake.SentText)
	}
	wantCalls := []string{"SendKeys(w1:p1,escape)", "SendText(w1:p1,continue)"}
	if !reflect.DeepEqual(fake.Calls, wantCalls) {
		t.Fatalf("Calls = %#v, want %#v", fake.Calls, wantCalls)
	}
}
