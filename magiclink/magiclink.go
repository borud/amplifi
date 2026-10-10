// Package magiclink controls MagicLink, the router side of Teleport
// (AmpliFi's remote access VPN) and of factory pairing of mesh points. Its
// methods speak JSON.
package magiclink

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// RouterConnectivity is the router's Teleport connectivity.
type RouterConnectivity struct {
	Local  string            `json:"local"`
	Remote map[string]string `json:"remote"`
	UPnP   string            `json:"upnp"`
}

// Connection is the state of the MagicLink connection itself.
type Connection struct {
	State           string            `json:"state"`
	Status          map[string]string `json:"status"`
	SameNetwork     string            `json:"same_network"`
	NetworkConflict string            `json:"network_conflict"`
}

// InviteCode is a Teleport invite code. Duration is in seconds and Expiration in
// Unix seconds.
type InviteCode struct {
	Code       string `json:"code,omitempty"`
	Duration   *int   `json:"duration,omitempty"`
	Sessions   *int   `json:"sessions,omitempty"`
	Success    *bool  `json:"success,omitempty"`
	Remaining  *int   `json:"remaining,omitempty"`
	Expiration *int64 `json:"expiration,omitempty"`
}

// InviteUpdate sets the validity of a new or the current invite code. Nil
// fields are left to the router. Duration is in seconds.
type InviteUpdate struct {
	Duration *int
	Sessions *int
}

// AccessMode is what a Teleport client may reach.
type AccessMode string

// Access modes of a Teleport client.
const (
	// AccessFull gives the client access to the LAN and the internet.
	AccessFull AccessMode = "FULL"
	// AccessWAN gives the client internet access only.
	AccessWAN AccessMode = "WAN"
)

// Client is a phone or computer that has joined the router over Teleport. Times are
// Unix seconds.
type Client struct {
	ID             string      `json:"id"`
	FriendlyName   string      `json:"friendly_name"`
	AccessMode     AccessMode  `json:"access_mode"`
	MAC            amplifi.MAC `json:"mac"`
	PublicIP       string      `json:"pub_ipv4_addr"`
	IP             string      `json:"ipv4_addr"`
	Online         bool        `json:"online"`
	ConnectionType string      `json:"conn_type"`
	OwnerDeviceID  string      `json:"owner_device_id"`
	Type           string      `json:"type"`
	Joined         int64       `json:"joined"`
	LastSeen       *int64      `json:"last_seen,omitempty"`
	Duration       *int64      `json:"duration,omitempty"`
	Platform       string      `json:"platform,omitempty"`
	State          string      `json:"state,omitempty"`
	RxRate         *int64      `json:"rx_rate,omitempty"`
	TxRate         *int64      `json:"tx_rate,omitempty"`
	Uptime         *int64      `json:"uptime,omitempty"`
}

// Pairing is a unit waiting to be paired with, or paired to, the router at
// the factory pairing step. PeerID is the router's key for it, which
// AcceptPairing and RejectPairing take. It is normally the unit's MAC, and
// MAC holds it parsed when it is one.
type Pairing struct {
	PeerID       string      `json:"peer_id"`
	MAC          amplifi.MAC `json:"mac"`
	FriendlyName string      `json:"friendly_name"`
	State        string      `json:"state"`
}

// RouterStatus reports the router's Teleport connectivity.
func RouterStatus(ctx context.Context, c *amplifi.Client) (*RouterConnectivity, error) {
	var s RouterConnectivity
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtGetConnectivityStatus", nil, &s); err != nil {
		return nil, fmt.Errorf("router status: %w", err)
	}
	if s.Remote == nil {
		s.Remote = map[string]string{}
	}
	return &s, nil
}

// Status reports the state of the MagicLink connection.
func Status(ctx context.Context, c *amplifi.Client) (*Connection, error) {
	var s Connection
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "MlGetConnectivityStatus", nil, &s); err != nil {
		return nil, fmt.Errorf("magiclink status: %w", err)
	}
	if s.Status == nil {
		s.Status = map[string]string{}
	}
	return &s, nil
}

// Invite returns the current invite code.
func Invite(ctx context.Context, c *amplifi.Client) (*InviteCode, error) {
	var inv InviteCode
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtReadInviteCode", nil, &inv); err != nil {
		return nil, fmt.Errorf("invite code: %w", err)
	}
	return &inv, nil
}

// CreateInvite generates a new invite code with u's settings and returns it.
func CreateInvite(ctx context.Context, c *amplifi.Client, u InviteUpdate) (*InviteCode, error) {
	return writeInvite(ctx, c, "RtGenerateInviteCode", u)
}

// UpdateInvite changes the current invite code's settings and returns it.
func UpdateInvite(ctx context.Context, c *amplifi.Client, u InviteUpdate) (*InviteCode, error) {
	return writeInvite(ctx, c, "RtUpdateInviteCode", u)
}

// RevokeInvite invalidates the current invite code.
func RevokeInvite(ctx context.Context, c *amplifi.Client) error {
	var inv InviteCode
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtRevokeInviteCode", nil, &inv); err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	if inv.Success != nil && !*inv.Success {
		return errors.New("revoke invite: router refused the change")
	}
	return nil
}

// Clients lists the Teleport clients, ordered by ID.
func Clients(ctx context.Context, c *amplifi.Client) ([]Client, error) {
	var m map[string]Client
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtGetClients", nil, &m); err != nil {
		return nil, fmt.Errorf("teleport clients: %w", err)
	}
	return clientList(m), nil
}

// RenameClient sets a Teleport client's friendly name.
func RenameClient(ctx context.Context, c *amplifi.Client, id, name string) error {
	return clientOp(ctx, c, "RtModifyClient", map[string]any{"id": id, "friendly_name": name})
}

// SetClientAccess sets a Teleport client's access mode.
func SetClientAccess(ctx context.Context, c *amplifi.Client, id string, mode AccessMode) error {
	return clientOp(ctx, c, "RtModifyClient", map[string]any{"id": id, "access_mode": string(mode)})
}

// DeleteClient removes a Teleport client.
func DeleteClient(ctx context.Context, c *amplifi.Client, id string) error {
	return clientOp(ctx, c, "RtRemoveClient", map[string]any{"id": id})
}

// Pairings returns the factory pairing state, ordered by peer ID.
func Pairings(ctx context.Context, c *amplifi.Client) ([]Pairing, error) {
	var m map[string]Pairing
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

func writeInvite(ctx context.Context, c *amplifi.Client, method string, u InviteUpdate) (*InviteCode, error) {
	arg := map[string]any{}
	if u.Duration != nil {
		arg["duration"] = *u.Duration
	}
	if u.Sessions != nil {
		arg["sessions"] = *u.Sessions
	}

	var inv InviteCode
	if err := rpc.JSON(ctx, c, amplifi.IfaceMagicLink, method, arg, &inv); err != nil {
		return nil, fmt.Errorf("invite code: %w", err)
	}
	return &inv, nil
}

// clientOp calls a client writer, which answers {"success": bool}.
func clientOp(ctx context.Context, c *amplifi.Client, method string, arg map[string]any) error {
	var res result
	if err := rpc.JSON(ctx, c, amplifi.IfaceMagicLink, method, arg, &res); err != nil {
		return fmt.Errorf("teleport client: %w", err)
	}
	return res.check(method)
}

// result is the answer of the client writers.
type result struct {
	Success bool `json:"success"`
}

func (r result) check(method string) error {
	if !r.Success {
		return fmt.Errorf("%s: router refused the change", method)
	}
	return nil
}

func clientList(m map[string]Client) []Client {
	out := make([]Client, 0, len(m))
	for id, cl := range m {
		cl.ID = id
		out = append(out, cl)
	}
	slices.SortFunc(out, func(a, b Client) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func pairingList(m map[string]Pairing) []Pairing {
	out := make([]Pairing, 0, len(m))
	for key, p := range m {
		p.PeerID = key
		p.MAC, _ = amplifi.ParseMAC(key)
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Pairing) int { return cmp.Compare(a.PeerID, b.PeerID) })
	return out
}
