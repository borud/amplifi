package mesh

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// Pairing is a unit waiting to be paired with, or paired to, the router at
// the factory pairing step. PeerID is the router's key for it, which
// AcceptPairing and RejectPairing take. It is normally the unit's MAC, and
// MAC holds it parsed when it is one.
type Pairing struct {
	PeerID string      `json:"peer_id"`
	MAC    amplifi.MAC `json:"mac"`
	Name   string      `json:"name"`
	State  string      `json:"state"`
}

// pairingEntry is one entry of RtGetFactoryPairingState.
type pairingEntry struct {
	FriendlyName string `json:"friendly_name"`
	State        string `json:"state"`
}

// Pairings returns the factory pairing state, ordered by peer ID.
func Pairings(ctx context.Context, c *amplifi.Client) ([]Pairing, error) {
	var m map[string]pairingEntry
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtGetFactoryPairingState", nil, &m); err != nil {
		return nil, fmt.Errorf("pairings: %w", err)
	}
	return pairingList(m), nil
}

// AcceptPairing accepts factory pairing with peerID.
func AcceptPairing(ctx context.Context, c *amplifi.Client, peerID string) error {
	if err := rpc.JSON(ctx, c, amplifi.IfaceMagicLink, "RtAcceptFactoryPairing", map[string]any{"peer_id": peerID}, nil); err != nil {
		return fmt.Errorf("accept pairing %q: %w", peerID, err)
	}
	return nil
}

// RejectPairing rejects factory pairing with peerID.
func RejectPairing(ctx context.Context, c *amplifi.Client, peerID string) error {
	if err := rpc.JSON(ctx, c, amplifi.IfaceMagicLink, "RtRejectFactoryPairing", map[string]any{"peer_id": peerID}, nil); err != nil {
		return fmt.Errorf("reject pairing %q: %w", peerID, err)
	}
	return nil
}

func pairingList(m map[string]pairingEntry) []Pairing {
	out := make([]Pairing, 0, len(m))
	for key, e := range m {
		mac, _ := amplifi.ParseMAC(key)
		out = append(out, Pairing{PeerID: key, MAC: mac, Name: e.FriendlyName, State: e.State})
	}
	slices.SortFunc(out, func(a, b Pairing) int { return cmp.Compare(a.PeerID, b.PeerID) })
	return out
}
