package internal

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
)

// reloadHub fans a change notification out to the browser pages showing the
// changed target. A target is a cleaned URL path: "/docs/a.md" for a markdown
// file, "/docs" for a directory and "/" for the root.
type reloadHub struct {
	mu   sync.Mutex
	subs map[string]map[*reloadSub]struct{}
}

type reloadSub struct {
	// C receives at most one pending notification.
	C      chan struct{}
	target string
}

func newReloadHub() *reloadHub {
	return &reloadHub{subs: make(map[string]map[*reloadSub]struct{})}
}

func (h *reloadHub) subscribe(target string) *reloadSub {
	sub := &reloadSub{C: make(chan struct{}, 1), target: target}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[target] == nil {
		h.subs[target] = make(map[*reloadSub]struct{})
	}
	h.subs[target][sub] = struct{}{}
	return sub
}

func (h *reloadHub) unsubscribe(sub *reloadSub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[sub.target], sub)
	if len(h.subs[sub.target]) == 0 {
		delete(h.subs, sub.target)
	}
}

// notify never blocks: a subscriber that already has a pending notification
// does not need another one.
func (h *reloadHub) notify(target string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs[target] {
		select {
		case sub.C <- struct{}{}:
		default:
		}
	}
}

func (h *reloadHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, subs := range h.subs {
		n += len(subs)
	}
	return n
}

// normalizeTarget turns the path the browser reports (a percent-encoded URL
// path) into a hub target.
func normalizeTarget(raw string) (string, error) {
	if !strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("target %q must start with '/'", raw)
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("target %q: %w", raw, err)
	}
	return path.Clean(decoded), nil
}
