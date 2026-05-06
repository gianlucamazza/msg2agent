package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// newTestPresenceManager creates a PresenceManager with a minimal hub
// (the hub is only used for notifying connected clients; tests that
// don't exercise notifications can use a hub with no live clients).
func newTestPresenceManager() *PresenceManager {
	hub := testHub()
	return hub.presence
}

// TestMarkOnlineIsOnline verifies that UpdatePresence with Online makes
// GetPresence report the correct status.
func TestMarkOnlineIsOnline(t *testing.T) {
	pm := newTestPresenceManager()
	defer pm.Stop()

	did := "did:wba:example.com:agent:alice"
	pm.UpdatePresence(did, registry.StatusOnline, "")

	info := pm.GetPresence(did)
	if info == nil {
		t.Fatal("GetPresence returned nil after UpdatePresence")
	}
	if info.Status != registry.StatusOnline {
		t.Errorf("Status = %q, want %q", info.Status, registry.StatusOnline)
	}
}

// TestMarkOfflineIsOffline verifies that transitioning to Offline is reflected.
func TestMarkOfflineIsOffline(t *testing.T) {
	pm := newTestPresenceManager()
	defer pm.Stop()

	did := "did:wba:example.com:agent:bob"
	pm.UpdatePresence(did, registry.StatusOnline, "")
	pm.UpdatePresence(did, registry.StatusOffline, "")

	info := pm.GetPresence(did)
	if info == nil {
		t.Fatal("GetPresence returned nil")
	}
	if info.Status != registry.StatusOffline {
		t.Errorf("Status = %q, want %q", info.Status, registry.StatusOffline)
	}
}

// TestPresenceUpdate_UnknownDIDReturnsNil verifies that querying a DID that was
// never registered returns nil.
func TestPresenceUpdate_UnknownDIDReturnsNil(t *testing.T) {
	pm := newTestPresenceManager()
	defer pm.Stop()

	info := pm.GetPresence("did:wba:example.com:agent:nobody")
	if info != nil {
		t.Errorf("expected nil for unknown DID, got %+v", info)
	}
}

// TestPresenceStatusText verifies that status text is stored and returned.
func TestPresenceStatusText(t *testing.T) {
	pm := newTestPresenceManager()
	defer pm.Stop()

	did := "did:wba:example.com:agent:charlie"
	pm.UpdatePresence(did, registry.StatusBusy, "in a meeting")

	info := pm.GetPresence(did)
	if info == nil {
		t.Fatal("GetPresence returned nil")
	}
	if info.StatusText != "in a meeting" {
		t.Errorf("StatusText = %q, want %q", info.StatusText, "in a meeting")
	}
}

// TestSubscribeUnsubscribe verifies basic subscribe/unsubscribe mechanics without
// live clients: subscriptions are stored and removed correctly.
func TestSubscribeUnsubscribe(t *testing.T) {
	pm := newTestPresenceManager()
	defer pm.Stop()

	subscriber := "did:wba:example.com:agent:sub"
	target := "did:wba:example.com:agent:target"

	pm.Subscribe(subscriber, target)

	// Subscription should be present.
	pm.subMu.RLock()
	subs := pm.subscriptions[subscriber]
	has := subs != nil && subs[target]
	pm.subMu.RUnlock()

	if !has {
		t.Error("subscription not recorded after Subscribe")
	}

	pm.Unsubscribe(subscriber, target)

	pm.subMu.RLock()
	subs2 := pm.subscriptions[subscriber]
	has2 := subs2 != nil && subs2[target]
	pm.subMu.RUnlock()

	if has2 {
		t.Error("subscription still present after Unsubscribe")
	}
}

// TestUnsubscribeAll verifies that all subscriptions for a subscriber are removed.
func TestUnsubscribeAll(t *testing.T) {
	pm := newTestPresenceManager()
	defer pm.Stop()

	subscriber := "did:wba:example.com:agent:sub"
	targets := []string{
		"did:wba:example.com:agent:t1",
		"did:wba:example.com:agent:t2",
		"did:wba:example.com:agent:t3",
	}
	for _, tgt := range targets {
		pm.Subscribe(subscriber, tgt)
	}

	pm.UnsubscribeAll(subscriber)

	pm.subMu.RLock()
	_, exists := pm.subscriptions[subscriber]
	pm.subMu.RUnlock()

	if exists {
		t.Error("subscriptions map still has entry after UnsubscribeAll")
	}
}

// TestTypingStateSetAndQuery verifies SetTyping / IsTyping.
func TestTypingStateSetAndQuery(t *testing.T) {
	pm := newTestPresenceManager()
	defer pm.Stop()

	sender := "did:wba:example.com:agent:alice"
	thread := "thread-abc"

	if pm.IsTyping(sender, thread) {
		t.Fatal("expected not typing before any state set")
	}

	pm.SetTyping(sender, thread, true)
	if !pm.IsTyping(sender, thread) {
		t.Error("expected typing = true after SetTyping(true)")
	}

	pm.SetTyping(sender, thread, false)
	if pm.IsTyping(sender, thread) {
		t.Error("expected typing = false after SetTyping(false)")
	}
}

// TestConcurrentUpdatePresence verifies that concurrent calls to UpdatePresence
// for the same DID from multiple goroutines do not cause data races.
// Run with: go test -race ./cmd/relay/
func TestConcurrentUpdatePresence(t *testing.T) {
	pm := newTestPresenceManager()
	defer pm.Stop()

	did := "did:wba:example.com:agent:concurrent"
	statuses := []registry.AgentStatus{
		registry.StatusOnline,
		registry.StatusOffline,
		registry.StatusBusy,
		registry.StatusAway,
		registry.StatusDND,
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			pm.UpdatePresence(did, statuses[i%len(statuses)], "")
			_ = pm.GetPresence(did)
		}(i)
	}
	wg.Wait()

	// After all goroutines finish the presence entry must exist.
	if pm.GetPresence(did) == nil {
		t.Error("presence entry missing after concurrent updates")
	}
}

// TestPresenceNotifiesSubscribers verifies that subscribers with a live
// SendCh receive a notification when the target's presence changes.
func TestPresenceNotifiesSubscribers(t *testing.T) {
	hub := testHub()
	pm := hub.presence
	defer pm.Stop()

	subscriberDID := "did:wba:example.com:agent:sub"
	targetDID := "did:wba:example.com:agent:target"

	// Register a synthetic client for the subscriber.
	subClient := testClient(hub, "sub-client-id", subscriberDID)
	hub.Register(subClient)
	defer hub.Unregister(subClient)

	// Subscribe to target.
	pm.Subscribe(subscriberDID, targetDID)

	// Change target's presence.
	pm.UpdatePresence(targetDID, registry.StatusOnline, "ready")

	// The subscriber's SendCh should receive the notification.
	select {
	case data := <-subClient.SendCh:
		if len(data) == 0 {
			t.Error("received empty notification")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("subscriber did not receive presence notification within 200ms")
	}
}

// TestMultipleSubscribersReceiveEvent verifies that all subscribers of a target
// receive the notification when the target updates its presence.
func TestMultipleSubscribersReceiveEvent(t *testing.T) {
	hub := testHub()
	pm := hub.presence
	defer pm.Stop()

	targetDID := "did:wba:example.com:agent:target2"
	const numSubscribers = 3

	clients := make([]*Client, numSubscribers)
	for i := range numSubscribers {
		did := fmt.Sprintf("did:wba:example.com:agent:sub%d", i)
		cl := testClient(hub, fmt.Sprintf("sub-id-%d", i), did)
		hub.Register(cl)
		defer hub.Unregister(cl)
		pm.Subscribe(did, targetDID)
		clients[i] = cl
	}

	pm.UpdatePresence(targetDID, registry.StatusBusy, "busy")

	// Each subscriber must receive a notification.
	for i, cl := range clients {
		select {
		case data := <-cl.SendCh:
			if len(data) == 0 {
				t.Errorf("subscriber %d: empty notification", i)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("subscriber %d did not receive notification", i)
		}
	}
}
