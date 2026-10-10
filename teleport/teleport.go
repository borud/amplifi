// Package teleport controls Teleport, AmpliFi's remote access VPN, through
// the router's MagicLink interface: connectivity, invite codes and the
// clients that have joined. Its methods speak JSON. Factory pairing of mesh
// points, which uses the same interface, is in package mesh.
package teleport

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// Connectivity is the router's Teleport connectivity.
type Connectivity struct {
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

// Invite is a Teleport invite code: how long it is valid, how many devices
// may join with it and how many still can.
type Invite struct {
	Code      string        `json:"code"`
	Duration  time.Duration `json:"duration"`
	Sessions  int           `json:"sessions"`
	Remaining int           `json:"remaining"`
	Expires   time.Time     `json:"expires,omitzero"`
}

// MarshalJSON renders Duration in seconds.
func (inv Invite) MarshalJSON() ([]byte, error) {
	type invite Invite
	return json.Marshal(struct {
		invite
		Duration int64 `json:"duration"`
	}{invite(inv), int64(inv.Duration / time.Second)})
}

// inviteEntry is an invite as the router sends it. Times are in seconds.
type inviteEntry struct {
	Code       string `json:"code"`
	Duration   int64  `json:"duration"`
	Sessions   int    `json:"sessions"`
	Remaining  int    `json:"remaining"`
	Expiration int64  `json:"expiration"`
	Success    *bool  `json:"success"`
}

// invite converts e, failing when the router reports no success.
func (e inviteEntry) invite() (*Invite, error) {
	if e.Success != nil && !*e.Success {
		return nil, errors.New("router refused the change")
	}
	inv := &Invite{
		Code:      e.Code,
		Duration:  time.Duration(e.Duration) * time.Second,
		Sessions:  e.Sessions,
		Remaining: e.Remaining,
	}
	if e.Expiration > 0 {
		inv.Expires = time.Unix(e.Expiration, 0)
	}
	return inv, nil
}

// InviteUpdate sets the validity of a new or the current invite code. Nil
// fields are left to the router. Duration is sent in whole seconds.
type InviteUpdate struct {
	Duration *time.Duration
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

// Client is a phone or computer that has joined the router over Teleport.
// The unit of RxRate and TxRate is unconfirmed.
type Client struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	AccessMode     AccessMode    `json:"access_mode"`
	MAC            amplifi.MAC   `json:"mac"`
	PublicIP       netip.Addr    `json:"public_ip"`
	IP             netip.Addr    `json:"ip"`
	Online         bool          `json:"online"`
	ConnectionType string        `json:"connection_type"`
	OwnerDeviceID  string        `json:"owner_device_id"`
	Type           string        `json:"type"`
	Joined         time.Time     `json:"joined,omitzero"`
	LastSeen       time.Time     `json:"last_seen,omitzero"`
	Duration       time.Duration `json:"duration"`
	Platform       string        `json:"platform,omitempty"`
	State          string        `json:"state,omitempty"`
	RxRate         int           `json:"rx_rate"`
	TxRate         int           `json:"tx_rate"`
	Uptime         time.Duration `json:"uptime"`
}

// MarshalJSON renders Duration and Uptime in seconds.
func (cl Client) MarshalJSON() ([]byte, error) {
	type client Client
	return json.Marshal(struct {
		client
		Duration int64 `json:"duration"`
		Uptime   int64 `json:"uptime"`
	}{client(cl), int64(cl.Duration / time.Second), int64(cl.Uptime / time.Second)})
}

// clientEntry is a client as the router sends it. Times are in seconds.
type clientEntry struct {
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
	LastSeen       int64       `json:"last_seen"`
	Duration       int64       `json:"duration"`
	Platform       string      `json:"platform"`
	State          string      `json:"state"`
	RxRate         int         `json:"rx_rate"`
	TxRate         int         `json:"tx_rate"`
	Uptime         int64       `json:"uptime"`
}

// client converts e. Addresses that do not parse are left empty.
func (e clientEntry) client(id string) Client {
	cl := Client{
		ID:             id,
		Name:           e.FriendlyName,
		AccessMode:     e.AccessMode,
		MAC:            e.MAC,
		Online:         e.Online,
		ConnectionType: e.ConnectionType,
		OwnerDeviceID:  e.OwnerDeviceID,
		Type:           e.Type,
		Joined:         unixTime(e.Joined),
		LastSeen:       unixTime(e.LastSeen),
		Duration:       time.Duration(e.Duration) * time.Second,
		Platform:       e.Platform,
		State:          e.State,
		RxRate:         e.RxRate,
		TxRate:         e.TxRate,
		Uptime:         time.Duration(e.Uptime) * time.Second,
	}
	cl.PublicIP, _ = netip.ParseAddr(e.PublicIP)
	cl.IP, _ = netip.ParseAddr(e.IP)
	return cl
}

// GetConnectivity reports the router's Teleport connectivity.
func GetConnectivity(ctx context.Context, c *amplifi.Client) (*Connectivity, error) {
	var s Connectivity
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtGetConnectivityStatus", nil, &s); err != nil {
		return nil, fmt.Errorf("teleport connectivity: %w", err)
	}
	if s.Remote == nil {
		s.Remote = map[string]string{}
	}
	return &s, nil
}

// GetConnection reports the state of the MagicLink connection.
func GetConnection(ctx context.Context, c *amplifi.Client) (*Connection, error) {
	var s Connection
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "MlGetConnectivityStatus", nil, &s); err != nil {
		return nil, fmt.Errorf("teleport connection: %w", err)
	}
	if s.Status == nil {
		s.Status = map[string]string{}
	}
	return &s, nil
}

// GetInvite returns the current invite code.
func GetInvite(ctx context.Context, c *amplifi.Client) (*Invite, error) {
	var e inviteEntry
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtReadInviteCode", nil, &e); err != nil {
		return nil, fmt.Errorf("invite code: %w", err)
	}
	inv, err := e.invite()
	if err != nil {
		return nil, fmt.Errorf("invite code: %w", err)
	}
	return inv, nil
}

// CreateInvite generates a new invite code with u's settings and returns it.
func CreateInvite(ctx context.Context, c *amplifi.Client, u InviteUpdate) (*Invite, error) {
	return writeInvite(ctx, c, "RtGenerateInviteCode", u)
}

// UpdateInvite changes the current invite code's settings and returns it.
func UpdateInvite(ctx context.Context, c *amplifi.Client, u InviteUpdate) (*Invite, error) {
	return writeInvite(ctx, c, "RtUpdateInviteCode", u)
}

// RevokeInvite invalidates the current invite code.
func RevokeInvite(ctx context.Context, c *amplifi.Client) error {
	var e inviteEntry
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtRevokeInviteCode", nil, &e); err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	if _, err := e.invite(); err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	return nil
}

// Clients lists the Teleport clients, ordered by ID.
func Clients(ctx context.Context, c *amplifi.Client) ([]Client, error) {
	var m map[string]clientEntry
	if err := rpc.Value(ctx, c, amplifi.IfaceMagicLink, "RtGetClients", nil, &m); err != nil {
		return nil, fmt.Errorf("teleport clients: %w", err)
	}
	return clientList(m), nil
}

// RenameClient sets a Teleport client's name.
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

func writeInvite(ctx context.Context, c *amplifi.Client, method string, u InviteUpdate) (*Invite, error) {
	arg := map[string]any{}
	if u.Duration != nil {
		arg["duration"] = int64(*u.Duration / time.Second)
	}
	if u.Sessions != nil {
		arg["sessions"] = *u.Sessions
	}

	var e inviteEntry
	if err := rpc.JSON(ctx, c, amplifi.IfaceMagicLink, method, arg, &e); err != nil {
		return nil, fmt.Errorf("invite code: %w", err)
	}
	inv, err := e.invite()
	if err != nil {
		return nil, fmt.Errorf("invite code: %w", err)
	}
	return inv, nil
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

func clientList(m map[string]clientEntry) []Client {
	out := make([]Client, 0, len(m))
	for id, e := range m {
		out = append(out, e.client(id))
	}
	slices.SortFunc(out, func(a, b Client) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// unixTime converts Unix seconds; 0 or less is the zero time.
func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
