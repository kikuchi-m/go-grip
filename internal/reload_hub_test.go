package internal

import (
	"testing"
	"time"
)

func received(sub *reloadSub) bool {
	select {
	case <-sub.C:
		return true
	default:
		return false
	}
}

func TestNormalizeTarget(t *testing.T) {
	t.Parallel()

	valid := []struct{ raw, want string }{
		{"/a.md", "/a.md"},
		{"/docs/", "/docs"},
		{"/docs/../a.md", "/a.md"},
		{"/%E6%97%A5%E6%9C%AC.md", "/日本.md"},
		{"/../x", "/x"},
		{"/", "/"},
	}
	for _, tt := range valid {
		got, err := normalizeTarget(tt.raw)
		if err != nil || got != tt.want {
			t.Errorf("normalizeTarget(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
		}
	}

	for _, raw := range []string{"", "a.md", "/%zz"} {
		if got, err := normalizeTarget(raw); err == nil {
			t.Errorf("normalizeTarget(%q) = %q, want an error", raw, got)
		}
	}
}

func TestHubNotifiesOnlyMatchingTarget(t *testing.T) {
	t.Parallel()

	hub := newReloadHub()
	a := hub.subscribe("/a.md")
	b := hub.subscribe("/b.md")

	hub.notify("/a.md")

	if !received(a) {
		t.Error("subscriber of /a.md was not notified")
	}
	if received(b) {
		t.Error("subscriber of /b.md must not be notified")
	}
}

func TestHubNotifyDoesNotBlock(t *testing.T) {
	t.Parallel()

	hub := newReloadHub()
	hub.subscribe("/a.md")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 3 {
			hub.notify("/a.md")
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notify blocked on a subscriber that does not read")
	}
}

func TestHubUnsubscribe(t *testing.T) {
	t.Parallel()

	hub := newReloadHub()
	sub := hub.subscribe("/a.md")
	if hub.count() != 1 {
		t.Fatalf("count = %d, want 1", hub.count())
	}

	hub.unsubscribe(sub)

	if hub.count() != 0 {
		t.Errorf("count after unsubscribe = %d, want 0", hub.count())
	}
	hub.notify("/a.md")
	if received(sub) {
		t.Error("unsubscribed subscriber was notified")
	}
}
