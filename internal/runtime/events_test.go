package runtime

import "testing"

func TestEventSourceCapabilityModelIsNeutral(t *testing.T) {
	var source EventSource
	if source != nil {
		t.Fatal("zero EventSource should be nil")
	}
	if EventOutputMatched == EventAgentStatus || EventResync == EventPaneMoved {
		t.Fatal("event kinds must be distinct")
	}
	spec := SubscribeSpec{PaneIDs: []string{"p1"}, MatchRegex: "limit", ReadSource: "detection", ReadLines: 200}
	if spec.PaneIDs[0] != "p1" || spec.MatchRegex != "limit" {
		t.Fatalf("spec = %#v", spec)
	}
}
