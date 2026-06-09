package agent

import (
	"encoding/json"

	"github.com/gianlucamazza/msg2agent/pkg/registry"
)

// resolvePeer fetches a single peer's record from the relay registry and adds
// it to the local store. It is used to recover from a race on reconnect: the
// relay flushes queued messages to a client right after registration, which can
// arrive before the client's startup peer discovery has loaded the sender's
// public keys. Without the sender in the local store, signature verification
// and body decryption fail spuriously. Resolving on demand makes message
// processing independent of discovery timing.
//
// Returns nil if the peer is now present in the local store.
func (a *Agent) resolvePeer(did string) error {
	discoverResult, err := a.CallRelay(a.ctx, "relay.discover", nil)
	if err != nil {
		return err
	}

	var peers []struct {
		DID         string `json:"did"`
		DisplayName string `json:"display_name"`
		PublicKeys  []struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Key     string `json:"key"`
			Purpose string `json:"purpose"`
		} `json:"public_keys"`
	}
	if err := json.Unmarshal(discoverResult, &peers); err != nil {
		return err
	}

	for _, peer := range peers {
		if peer.DID != did {
			continue
		}
		var peerKeys []registry.PeerKey
		for _, k := range peer.PublicKeys {
			peerKeys = append(peerKeys, registry.PeerKey{
				ID:      k.ID,
				Type:    k.Type,
				Key:     k.Key,
				Purpose: k.Purpose,
			})
		}
		return a.Discovery().AddPeer(peer.DID, peer.DisplayName, peerKeys)
	}
	return ErrSenderNotFound
}
