package registry

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewChannel(t *testing.T) {
	channel := NewChannel("channel:test:general", ChannelGroup, "did:wba:alice")

	if channel.Name != "channel:test:general" {
		t.Errorf("expected name 'channel:test:general', got %q", channel.Name)
	}
	if channel.Type != ChannelGroup {
		t.Errorf("expected type group, got %s", channel.Type)
	}
	if channel.OwnerDID != "did:wba:alice" {
		t.Errorf("expected owner 'did:wba:alice', got %q", channel.OwnerDID)
	}
	if len(channel.Members) != 1 {
		t.Errorf("expected 1 member (owner), got %d", len(channel.Members))
	}
	if !channel.IsOwner("did:wba:alice") {
		t.Error("alice should be owner")
	}
	if !channel.IsMember("did:wba:alice") {
		t.Error("alice should be member")
	}
}

func TestChannelAddRemoveMember(t *testing.T) {
	channel := NewChannel("channel:test:general", ChannelGroup, "did:wba:alice")

	// Add member
	err := channel.AddMember("did:wba:bob", RoleMember)
	if err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	if !channel.IsMember("did:wba:bob") {
		t.Error("bob should be member after add")
	}

	// Try to add duplicate
	err = channel.AddMember("did:wba:bob", RoleMember)
	if err != ErrMemberAlreadyAdded {
		t.Errorf("expected ErrMemberAlreadyAdded, got %v", err)
	}

	// Remove member
	removed := channel.RemoveMember("did:wba:bob")
	if !removed {
		t.Error("RemoveMember should return true")
	}
	if channel.IsMember("did:wba:bob") {
		t.Error("bob should not be member after remove")
	}

	// Remove non-member
	removed = channel.RemoveMember("did:wba:charlie")
	if removed {
		t.Error("RemoveMember should return false for non-member")
	}
}

func TestChannelRoles(t *testing.T) {
	channel := NewChannel("channel:test:general", ChannelGroup, "did:wba:alice")
	_ = channel.AddMember("did:wba:bob", RoleAdmin)
	_ = channel.AddMember("did:wba:charlie", RoleMember)

	// Owner is admin
	if !channel.IsAdmin("did:wba:alice") {
		t.Error("owner should be admin")
	}

	// Admin is admin
	if !channel.IsAdmin("did:wba:bob") {
		t.Error("admin should be admin")
	}

	// Member is not admin
	if channel.IsAdmin("did:wba:charlie") {
		t.Error("member should not be admin")
	}
}

func TestChannelCanSend(t *testing.T) {
	// Group channel: all members can send
	group := NewChannel("channel:test:group", ChannelGroup, "did:wba:alice")
	_ = group.AddMember("did:wba:bob", RoleMember)

	if !group.CanSend("did:wba:alice") {
		t.Error("owner should be able to send in group")
	}
	if !group.CanSend("did:wba:bob") {
		t.Error("member should be able to send in group")
	}
	if group.CanSend("did:wba:charlie") {
		t.Error("non-member should not be able to send")
	}

	// Broadcast channel: only owner can send
	broadcast := NewChannel("channel:test:broadcast", ChannelBroadcast, "did:wba:alice")
	_ = broadcast.AddMember("did:wba:bob", RoleMember)

	if !broadcast.CanSend("did:wba:alice") {
		t.Error("owner should be able to send in broadcast")
	}
	if broadcast.CanSend("did:wba:bob") {
		t.Error("member should not be able to send in broadcast")
	}
}

func TestChannelSenderKey(t *testing.T) {
	channel := NewChannel("channel:test:e2e", ChannelGroup, "did:wba:alice")

	key := &SenderKey{
		ChainKey:     []byte("chain-key"),
		SignatureKey: []byte("sig-key"),
		Iteration:    0,
	}

	if !channel.SetSenderKey("did:wba:alice", key) {
		t.Error("SetSenderKey should return true")
	}

	got := channel.GetSenderKey("did:wba:alice")
	if got == nil {
		t.Fatal("GetSenderKey returned nil")
	}
	if string(got.ChainKey) != "chain-key" {
		t.Error("chain key mismatch")
	}

	// Non-member
	if channel.SetSenderKey("did:wba:nobody", key) {
		t.Error("SetSenderKey should return false for non-member")
	}
	if channel.GetSenderKey("did:wba:nobody") != nil {
		t.Error("GetSenderKey should return nil for non-member")
	}
}

func TestChannelMemberDIDs(t *testing.T) {
	channel := NewChannel("channel:test:general", ChannelGroup, "did:wba:alice")
	_ = channel.AddMember("did:wba:bob", RoleMember)
	_ = channel.AddMember("did:wba:charlie", RoleMember)

	dids := channel.MemberDIDs()
	if len(dids) != 3 {
		t.Errorf("expected 3 DIDs, got %d", len(dids))
	}
}

func TestMemoryChannelStore(t *testing.T) {
	store := NewMemoryChannelStore()

	// Create
	channel := NewChannel("channel:test:general", ChannelGroup, "did:wba:alice")
	if err := store.CreateChannel(channel); err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	// Create duplicate
	if err := store.CreateChannel(channel); err != ErrChannelExists {
		t.Errorf("expected ErrChannelExists, got %v", err)
	}

	// Get by ID
	got, err := store.GetChannel(channel.ID)
	if err != nil {
		t.Fatalf("GetChannel failed: %v", err)
	}
	if got.Name != channel.Name {
		t.Error("channel name mismatch")
	}

	// Get by name
	got, err = store.GetChannelByName("channel:test:general")
	if err != nil {
		t.Fatalf("GetChannelByName failed: %v", err)
	}
	if got.ID != channel.ID {
		t.Error("channel ID mismatch")
	}

	// Not found
	_, err = store.GetChannel(uuid.Must(uuid.NewV7()))
	if err != ErrChannelNotFound {
		t.Errorf("expected ErrChannelNotFound, got %v", err)
	}

	// Update
	channel.Description = "Updated"
	if err := store.UpdateChannel(channel); err != nil {
		t.Fatalf("UpdateChannel failed: %v", err)
	}
	got, _ = store.GetChannel(channel.ID)
	if got.Description != "Updated" {
		t.Error("update not applied")
	}

	// List
	_ = channel.AddMember("did:wba:bob", RoleMember)
	_ = store.UpdateChannel(channel)

	channels, err := store.ListChannels("did:wba:alice")
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if len(channels) != 1 {
		t.Errorf("expected 1 channel for alice, got %d", len(channels))
	}

	channels, _ = store.ListChannels("did:wba:bob")
	if len(channels) != 1 {
		t.Errorf("expected 1 channel for bob, got %d", len(channels))
	}

	channels, _ = store.ListChannels("did:wba:charlie")
	if len(channels) != 0 {
		t.Errorf("expected 0 channels for charlie, got %d", len(channels))
	}

	// Delete
	if err := store.DeleteChannel(channel.ID); err != nil {
		t.Fatalf("DeleteChannel failed: %v", err)
	}
	_, err = store.GetChannel(channel.ID)
	if err != ErrChannelNotFound {
		t.Error("channel should be deleted")
	}
}
