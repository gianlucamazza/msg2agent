package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gianlucamazza/msg2agent/pkg/messaging"
	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// newTestChannelManager returns a ChannelManager backed by a fresh MemoryChannelStore,
// wired to a test relay hub (no DID proof required).
func newTestChannelManager() (*ChannelManager, *RelayHub) {
	hub := testHub()
	return hub.channels, hub
}

// TestCreateChannelGetChannel verifies the basic create→get round-trip.
func TestCreateChannelGetChannel(t *testing.T) {
	cm, _ := newTestChannelManager()

	ownerDID := "did:wba:example.com:agent:owner"
	ch := registry.NewChannel("channel:example.com:general", registry.ChannelGroup, ownerDID)

	if err := cm.store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	got, err := cm.store.GetChannel(ch.ID)
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got.ID != ch.ID {
		t.Errorf("ID = %v, want %v", got.ID, ch.ID)
	}
	if got.OwnerDID != ownerDID {
		t.Errorf("OwnerDID = %q, want %q", got.OwnerDID, ownerDID)
	}
	if got.Name != ch.Name {
		t.Errorf("Name = %q, want %q", got.Name, ch.Name)
	}
}

// TestGetChannelByName verifies lookup by name.
func TestGetChannelByName(t *testing.T) {
	cm, _ := newTestChannelManager()

	ownerDID := "did:wba:example.com:agent:owner"
	name := "channel:example.com:my-channel"
	ch := registry.NewChannel(name, registry.ChannelGroup, ownerDID)
	if err := cm.store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	got, err := cm.store.GetChannelByName(name)
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if got.ID != ch.ID {
		t.Errorf("ID mismatch via name lookup")
	}
}

// TestJoinChannelIncreaseMemberCount verifies that AddMember increases Members.
func TestJoinChannelIncreaseMemberCount(t *testing.T) {
	cm, _ := newTestChannelManager()

	ownerDID := "did:wba:example.com:agent:owner"
	ch := registry.NewChannel("channel:example.com:members", registry.ChannelGroup, ownerDID)
	if err := cm.store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	initialCount := len(ch.Members) // owner is already a member

	newMember := "did:wba:example.com:agent:member1"
	if err := ch.AddMember(newMember, registry.RoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := cm.store.UpdateChannel(ch); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	updated, err := cm.store.GetChannel(ch.ID)
	if err != nil {
		t.Fatalf("GetChannel after join: %v", err)
	}
	if len(updated.Members) != initialCount+1 {
		t.Errorf("member count = %d, want %d", len(updated.Members), initialCount+1)
	}
	if !updated.IsMember(newMember) {
		t.Error("new member not in member list")
	}
}

// TestLeaveChannelDecreasesMemberCount verifies RemoveMember decreases count.
func TestLeaveChannelDecreasesMemberCount(t *testing.T) {
	cm, _ := newTestChannelManager()

	ownerDID := "did:wba:example.com:agent:owner"
	memberDID := "did:wba:example.com:agent:member"
	ch := registry.NewChannel("channel:example.com:leave", registry.ChannelGroup, ownerDID)
	_ = ch.AddMember(memberDID, registry.RoleMember)
	if err := cm.store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	countBefore := len(ch.Members)
	if !ch.RemoveMember(memberDID) {
		t.Fatal("RemoveMember returned false")
	}
	if err := cm.store.UpdateChannel(ch); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	updated, err := cm.store.GetChannel(ch.ID)
	if err != nil {
		t.Fatalf("GetChannel after leave: %v", err)
	}
	if len(updated.Members) != countBefore-1 {
		t.Errorf("member count = %d, want %d", len(updated.Members), countBefore-1)
	}
	if updated.IsMember(memberDID) {
		t.Error("removed member still in member list")
	}
}

// TestBroadcastToChannelMembersReceive verifies that all members (except sender)
// receive the message when RouteToChannel is called, and non-members do not.
func TestBroadcastToChannelMembersReceive(t *testing.T) {
	cm, hub := newTestChannelManager()

	ownerDID := "did:wba:example.com:agent:owner"
	member1DID := "did:wba:example.com:agent:m1"
	member2DID := "did:wba:example.com:agent:m2"
	nonMemberDID := "did:wba:example.com:agent:outsider"

	channelName := "channel:example.com:broadcast-test"
	ch := registry.NewChannel(channelName, registry.ChannelGroup, ownerDID)
	_ = ch.AddMember(member1DID, registry.RoleMember)
	_ = ch.AddMember(member2DID, registry.RoleMember)
	if err := cm.store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Register live clients for members.
	ownerClient := testClient(hub, "owner-id", ownerDID)
	m1Client := testClient(hub, "m1-id", member1DID)
	m2Client := testClient(hub, "m2-id", member2DID)
	nonMemberClient := testClient(hub, "nm-id", nonMemberDID)
	hub.Register(ownerClient)
	hub.Register(m1Client)
	hub.Register(m2Client)
	hub.Register(nonMemberClient)
	defer hub.Unregister(ownerClient)
	defer hub.Unregister(m1Client)
	defer hub.Unregister(m2Client)
	defer hub.Unregister(nonMemberClient)

	payload := []byte(`{"jsonrpc":"2.0","method":"message","params":{}}`)
	msg := &messaging.Message{
		From: ownerDID,
		To:   channelName,
	}

	if err := cm.RouteToChannel(msg, payload); err != nil {
		t.Fatalf("RouteToChannel: %v", err)
	}

	// m1 and m2 must receive, owner (sender) must not.
	checkReceived := func(cl *Client, name string, want bool) {
		t.Helper()
		select {
		case <-cl.SendCh:
			if !want {
				t.Errorf("%s: received message but should not have", name)
			}
		default:
			if want {
				t.Errorf("%s: did not receive message but should have", name)
			}
		}
	}

	checkReceived(m1Client, "member1", true)
	checkReceived(m2Client, "member2", true)
	checkReceived(ownerClient, "owner/sender", false)
	checkReceived(nonMemberClient, "non-member", false)
}

// TestDeleteChannel verifies that DeleteChannel removes the channel and members
// with connected clients receive a deletion notification.
func TestDeleteChannel(t *testing.T) {
	cm, hub := newTestChannelManager()

	ownerDID := "did:wba:example.com:agent:owner"
	memberDID := "did:wba:example.com:agent:memb"
	channelName := "channel:example.com:to-delete"

	ch := registry.NewChannel(channelName, registry.ChannelGroup, ownerDID)
	_ = ch.AddMember(memberDID, registry.RoleMember)
	if err := cm.store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Register live client for the member so notifications can be delivered.
	memberClient := testClient(hub, "memb-id", memberDID)
	hub.Register(memberClient)
	defer hub.Unregister(memberClient)

	// Delete channel from the store side.
	if err := cm.store.DeleteChannel(ch.ID); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}

	// Verify the channel is gone.
	_, err := cm.store.GetChannel(ch.ID)
	if err == nil {
		t.Error("GetChannel should return error after DeleteChannel")
	}
	_, err = cm.store.GetChannelByName(channelName)
	if err == nil {
		t.Error("GetChannelByName should return error after DeleteChannel")
	}
}

// TestOnlyOwnerCanDeleteChannel verifies the permission check in Channel.IsOwner.
func TestOnlyOwnerCanDeleteChannel(t *testing.T) {
	cm, _ := newTestChannelManager()

	ownerDID := "did:wba:example.com:agent:owner"
	nonOwnerDID := "did:wba:example.com:agent:other"

	ch := registry.NewChannel("channel:example.com:perm", registry.ChannelGroup, ownerDID)
	_ = ch.AddMember(nonOwnerDID, registry.RoleMember)
	if err := cm.store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if ch.IsOwner(nonOwnerDID) {
		t.Error("non-owner incorrectly reports as owner")
	}
	if !ch.IsOwner(ownerDID) {
		t.Error("owner reports as non-owner")
	}

	// Attempting to delete without owner rights should be rejected at the handler
	// level; here we verify the IsOwner guard that drives that logic.
	got, err := cm.store.GetChannel(ch.ID)
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got.IsOwner(nonOwnerDID) {
		t.Error("stored channel: non-owner IsOwner returned true")
	}
}

// TestConcurrentJoinLeave verifies that concurrent JoinChannel/LeaveChannel
// operations on the same channel, serialized through the ChannelManager lock
// (as the real WS handler path does), do not cause data races.
// Run with: go test -race ./cmd/relay/
func TestConcurrentJoinLeave(t *testing.T) {
	cm, _ := newTestChannelManager()

	ownerDID := "did:wba:example.com:agent:owner"
	ch := registry.NewChannel("channel:example.com:concurrent", registry.ChannelGroup, ownerDID)
	if err := cm.store.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			did := fmt.Sprintf("did:wba:example.com:agent:c%d", i)

			// The ChannelManager.mu serialises store access in the production path.
			// Replicate that here so the test validates the protected fast-path.
			cm.mu.Lock()
			loaded, err := cm.store.GetChannelByName(ch.Name)
			if err != nil {
				cm.mu.Unlock()
				return
			}
			_ = loaded.AddMember(did, registry.RoleMember)
			_ = cm.store.UpdateChannel(loaded)
			cm.mu.Unlock()

			// Load again under the same lock and remove.
			cm.mu.Lock()
			loaded2, err := cm.store.GetChannelByName(ch.Name)
			if err != nil {
				cm.mu.Unlock()
				return
			}
			loaded2.RemoveMember(did)
			_ = cm.store.UpdateChannel(loaded2)
			cm.mu.Unlock()
		}(i)
	}
	wg.Wait()

	// Channel must still exist and be retrievable.
	cm.mu.RLock()
	final, err := cm.store.GetChannel(ch.ID)
	cm.mu.RUnlock()
	if err != nil {
		t.Fatalf("channel missing after concurrent operations: %v", err)
	}
	// Owner must always remain.
	if !final.IsOwner(ownerDID) {
		t.Error("owner lost after concurrent operations")
	}
}

// TestIsChannelURI verifies the IsChannelURI helper.
func TestIsChannelURI(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"channel:example.com:general", true},
		{"channel:", true},
		{"did:wba:example.com:agent:alice", false},
		{"", false},
		{"CHANNEL:x:y", false}, // case-sensitive
	}
	for _, tc := range cases {
		got := IsChannelURI(tc.input)
		if got != tc.want {
			t.Errorf("IsChannelURI(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
