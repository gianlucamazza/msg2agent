package main

import (
	"errors"
	"testing"

	"github.com/gianluca/msg2agent/pkg/messaging"
)

func TestValidateSender_NotRegistered(t *testing.T) {
	client := &Client{
		ID:  "test-client-id",
		DID: "", // Not registered
	}

	msg := &messaging.Message{
		From: "did:wba:example.com:agent:alice",
		To:   "did:wba:example.com:agent:bob",
	}

	err := client.validateSender(msg)
	if !errors.Is(err, ErrClientNotRegistered) {
		t.Errorf("expected ErrClientNotRegistered, got %v", err)
	}
}

func TestValidateSender_Mismatch(t *testing.T) {
	client := &Client{
		ID:  "test-client-id",
		DID: "did:wba:example.com:agent:alice",
	}

	// Message from different DID
	msg := &messaging.Message{
		From: "did:wba:example.com:agent:eve",
		To:   "did:wba:example.com:agent:bob",
	}

	err := client.validateSender(msg)
	if !errors.Is(err, ErrSenderMismatch) {
		t.Errorf("expected ErrSenderMismatch, got %v", err)
	}
}

func TestValidateSender_Valid(t *testing.T) {
	client := &Client{
		ID:  "test-client-id",
		DID: "did:wba:example.com:agent:alice",
	}

	msg := &messaging.Message{
		From: "did:wba:example.com:agent:alice",
		To:   "did:wba:example.com:agent:bob",
	}

	err := client.validateSender(msg)
	if err != nil {
		t.Errorf("validation should succeed, got %v", err)
	}
}

func TestValidateSender_PreventsSpoofing(t *testing.T) {
	// Scenario: Attacker tries to send message as admin
	client := &Client{
		ID:  "attacker-connection",
		DID: "did:wba:malicious.com:agent:attacker",
	}

	// Attacker tries to impersonate admin
	msg := &messaging.Message{
		From: "did:wba:trusted.com:agent:admin",
		To:   "did:wba:trusted.com:agent:database",
	}

	err := client.validateSender(msg)
	if err == nil {
		t.Error("spoofing attempt should be rejected")
	}
	if !errors.Is(err, ErrSenderMismatch) {
		t.Errorf("expected ErrSenderMismatch, got %v", err)
	}
}
