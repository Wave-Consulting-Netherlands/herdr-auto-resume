package coordinator

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type signalWriter struct {
	bytes.Buffer
	writes chan struct{}
}

func (w *signalWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	w.writes <- struct{}{}
	return n, err
}

func TestRunLoopPollsLogsNewActionOnceAndStopsOnCancel(t *testing.T) {
	fake := &runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1"}},
		Content:   map[string]string{"p1": "┌────┐\n> <<<TEST>>>"},
	}
	c := New(fake, Config{TestPattern: "<<<TEST>>>", DryRun: true})
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.EnableAll()

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 2)
	refreshed := make(chan struct{}, 2)
	log := &signalWriter{writes: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		c.RunLoop(ctx, ticks, func() ([]runtime.Pane, error) {
			refreshed <- struct{}{}
			return fake.PanesList, nil
		}, log)
		close(done)
	}()
	ticks <- time.Unix(1, 0)
	<-refreshed
	<-log.writes
	ticks <- time.Unix(2, 0)
	<-refreshed
	cancel()
	<-done

	if len(fake.SentText) != 0 || len(fake.SentKeys) != 0 {
		t.Fatalf("dry-run sent text=%#v keys=%#v", fake.SentText, fake.SentKeys)
	}
	if got := strings.Count(log.String(), "pane=p1"); got != 1 {
		t.Fatalf("action log count = %d, log=%q", got, log.String())
	}
	if !strings.Contains(log.String(), "continue") || !strings.Contains(log.String(), "DRY-RUN") {
		t.Fatalf("log = %q, want action and dry-run marker", log.String())
	}
}

func TestRunLoopLogsRefreshErrorsAndNotifiesRealActions(t *testing.T) {
	fake := &runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1"}},
		Content:   map[string]string{"p1": "┌────┐\n> <<<TEST>>>"},
	}
	c := New(fake, Config{TestPattern: "<<<TEST>>>"})
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.EnableAll()

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 1)
	log := &signalWriter{writes: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		c.RunLoop(ctx, ticks, func() ([]runtime.Pane, error) {
			return nil, context.DeadlineExceeded
		}, log)
		close(done)
	}()
	ticks <- time.Unix(3, 0)
	<-log.writes
	cancel()
	<-done

	if !strings.Contains(log.String(), "refresh failed") {
		t.Fatalf("log = %q, want refresh failure", log.String())
	}
}

func TestRunLoopNotifiesRealActionOnce(t *testing.T) {
	fake := &runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1"}},
		Content:   map[string]string{"p1": "┌────┐\n> <<<TEST>>>"},
	}
	c := New(fake, Config{TestPattern: "<<<TEST>>>"})
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.EnableAll()

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 2)
	refreshed := make(chan struct{}, 2)
	log := &signalWriter{writes: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		c.RunLoop(ctx, ticks, func() ([]runtime.Pane, error) {
			refreshed <- struct{}{}
			return fake.PanesList, nil
		}, log)
		close(done)
	}()
	ticks <- time.Unix(4, 0)
	<-refreshed
	<-log.writes
	ticks <- time.Unix(5, 0)
	<-refreshed
	cancel()
	<-done

	if len(fake.Notes) != 1 || fake.Notes[0].Title != "auto-resume" || fake.Notes[0].Body != "continued pane p1" {
		t.Fatalf("notifications = %#v, want one auto-resume notification", fake.Notes)
	}
}

func TestRunLoopCallsPostPollWithInjectedClockAfterEachSuccessfulPoll(t *testing.T) {
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "plain shell"}}
	now := time.Unix(42, 0)
	var observed []time.Time
	postPoll := make(chan time.Time, 2)
	c := New(fake, Config{}, WithClock(func() time.Time { return now }), WithPostPoll(func(got time.Time) {
		observed = append(observed, got)
		postPoll <- got
	}))
	c.SetPanes(fake.PanesList)
	c.Poll()

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 2)
	done := make(chan struct{})
	go func() {
		c.RunLoop(ctx, ticks, func() ([]runtime.Pane, error) { return fake.PanesList, nil }, nil)
		close(done)
	}()
	ticks <- time.Unix(1, 0)
	ticks <- time.Unix(2, 0)
	<-postPoll
	<-postPoll
	cancel()
	<-done
	if !reflect.DeepEqual(observed, []time.Time{now, now}) {
		t.Fatalf("postPoll times = %#v, want %#v", observed, []time.Time{now, now})
	}
}
