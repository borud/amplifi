// Package dhcp reads and changes a unit's DHCP server: the address pools of
// each network and the static leases (address reservations).
package dhcp

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
	"github.com/borud/amplifi/internal/wire"
	"github.com/vmihailenco/msgpack/v5"
)

// DhcpPoolConfigOption codes.
const (
	poolInterface = "1"
	poolIgnore    = "2"
	poolOffset    = "3"
	poolLength    = "4"
	poolLeaseTime = "5"
	poolDHCP4Mode = "6"
	poolDHCP6Mode = "7"
	poolRAMode    = "8"
	poolNDPMode   = "9"
)

// DhcpStaticLeaseConfigOption codes.
const (
	leaseName        = "1"
	leaseMAC         = "2"
	leaseAddress     = "3"
	leaseDescription = "4"
)

// modeNames maps DhcpMode codes.
var modeNames = map[string]string{"1": "disabled", "2": "server", "3": "relay", "4": "hybrid"}

func modeCode(s string) (int64, error) {
	return rpc.Code("DHCP mode", modeNames, s)
}

func modeName(m map[string]any, key string) string {
	n := rpc.Int(m, key)
	if n <= 0 {
		return ""
	}
	return rpc.CodeName(modeNames, n)
}

// Pool is the DHCP server configuration of one network (GetDhcpPools).
// Addresses are handed out from Offset to Offset+Length-1 within the
// network's subnet. Modes are "disabled", "server", "relay" or "hybrid".
// LeaseTime is the router's raw value; its unit is unconfirmed, probably
// seconds.
type Pool struct {
	Name      string `json:"name"`
	Interface string `json:"interface"`
	Ignore    bool   `json:"ignore"`
	Offset    int    `json:"offset"`
	Length    int    `json:"length"`
	LeaseTime int    `json:"lease_time"`
	DHCP4Mode string `json:"dhcp4_mode,omitempty"`
	DHCP6Mode string `json:"dhcp6_mode,omitempty"`
	RAMode    string `json:"ra_mode,omitempty"`
	NDPMode   string `json:"ndp_mode,omitempty"`
}

// update returns the set fields of p as an update, for a new pool. The
// interface defaults to the pool name.
func (p Pool) update() PoolUpdate {
	return PoolUpdate{
		Interface: nonZero(cmp.Or(p.Interface, p.Name)),
		Ignore:    &p.Ignore,
		Offset:    nonZero(p.Offset),
		Length:    nonZero(p.Length),
		LeaseTime: nonZero(p.LeaseTime),
		DHCP4Mode: nonZero(p.DHCP4Mode),
		DHCP6Mode: nonZero(p.DHCP6Mode),
		RAMode:    nonZero(p.RAMode),
		NDPMode:   nonZero(p.NDPMode),
	}
}

// nonZero returns &v, or nil when v is the zero value.
func nonZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// Pools lists the DHCP pools, sorted by name.
func Pools(ctx context.Context, c *amplifi.Client) ([]Pool, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetDhcpPools", nil)
	if err != nil {
		return nil, fmt.Errorf("DHCP pools: %w", err)
	}
	return parsePools(m), nil
}

// parsePools decodes DhcpPools: a map of pool name to DhcpPoolConfigOption
// map.
func parsePools(m map[string]any) []Pool {
	out := make([]Pool, 0, len(m))
	for _, name := range slices.Sorted(maps.Keys(m)) {
		f, ok := m[name].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Pool{
			Name:      name,
			Interface: rpc.String(f, poolInterface),
			Ignore:    rpc.Bool(f, poolIgnore),
			Offset:    int(rpc.Int(f, poolOffset)),
			Length:    int(rpc.Int(f, poolLength)),
			LeaseTime: int(rpc.Int(f, poolLeaseTime)),
			DHCP4Mode: modeName(f, poolDHCP4Mode),
			DHCP6Mode: modeName(f, poolDHCP6Mode),
			RAMode:    modeName(f, poolRAMode),
			NDPMode:   modeName(f, poolNDPMode),
		})
	}
	return out
}

// PoolUpdate lists the pool settings to change; nil fields are left as they
// are. Modes are as in Pool.
type PoolUpdate struct {
	Interface *string
	Ignore    *bool
	Offset    *int
	Length    *int
	LeaseTime *int
	DHCP4Mode *string
	DHCP6Mode *string
	RAMode    *string
	NDPMode   *string
}

func (u PoolUpdate) options() (map[string]any, error) {
	m := map[string]any{}
	rpc.Put(m, poolInterface, u.Interface)
	rpc.Put(m, poolIgnore, u.Ignore)
	rpc.PutInt(m, poolOffset, u.Offset)
	rpc.PutInt(m, poolLength, u.Length)
	rpc.PutInt(m, poolLeaseTime, u.LeaseTime)
	for key, mode := range map[string]*string{
		poolDHCP4Mode: u.DHCP4Mode,
		poolDHCP6Mode: u.DHCP6Mode,
		poolRAMode:    u.RAMode,
		poolNDPMode:   u.NDPMode,
	} {
		if mode == nil {
			continue
		}
		code, err := modeCode(*mode)
		if err != nil {
			return nil, err
		}
		m[key] = code
	}
	return m, nil
}

// AddPool adds pool p, named after its network interface such as "lan".
// Zero and empty fields are left to the router, except Ignore.
func AddPool(ctx context.Context, c *amplifi.Client, p Pool) error {
	f, err := p.update().options()
	if err != nil {
		return fmt.Errorf("DHCP pool %q: %w", p.Name, err)
	}
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "AddDhcpPools", map[string]any{p.Name: f}); err != nil {
		return fmt.Errorf("add DHCP pool %q: %w", p.Name, err)
	}
	return nil
}

// UpdatePool changes the pool called name. It reads all pools, applies u to
// that one and writes them all back.
func UpdatePool(ctx context.Context, c *amplifi.Client, name string, u PoolUpdate) error {
	return updatePool(ctx, c, name, u)
}

func updatePool(ctx context.Context, c rpc.Caller, name string, u PoolUpdate) error {
	changes, err := u.options()
	if err != nil {
		return fmt.Errorf("DHCP pool %q: %w", name, err)
	}
	if err := rpc.UpdateEntry(ctx, c, amplifi.IfaceDevice, "GetDhcpPools", nil, "SetDhcpPools", name, changes); err != nil {
		return fmt.Errorf("DHCP pool %q: %w", name, err)
	}
	return nil
}

// DeletePools deletes pools by name.
func DeletePools(ctx context.Context, c *amplifi.Client, names ...string) error {
	arg := make([]any, 0, len(names))
	for _, n := range names {
		arg = append(arg, n)
	}
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "DeleteDhcpPools", arg); err != nil {
		return fmt.Errorf("delete DHCP pools: %w", err)
	}
	return nil
}

// StaticLease is a fixed address reservation (DhcpStaticLease).
type StaticLease struct {
	MAC         amplifi.MAC `json:"mac"`
	IP          netip.Addr  `json:"ip"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
}

// StaticLeases lists the static leases, sorted by name then MAC.
func StaticLeases(ctx context.Context, c *amplifi.Client) ([]StaticLease, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetDhcpStaticLeases", nil)
	if err != nil {
		return nil, fmt.Errorf("DHCP static leases: %w", err)
	}
	return parseStaticLeases(m), nil
}

// parseStaticLeases decodes DhcpStaticLeases: a map keyed by binary MAC of
// DhcpStaticLeaseConfigOption maps.
func parseStaticLeases(m map[string]any) []StaticLease {
	out := make([]StaticLease, 0, len(m))
	for key, v := range m {
		f, ok := v.(map[string]any)
		mac := rpc.MAC(key)
		if !ok || mac == nil {
			continue
		}
		out = append(out, StaticLease{
			MAC:         mac,
			IP:          rpc.Addr(f[leaseAddress]),
			Name:        rpc.String(f, leaseName),
			Description: rpc.String(f, leaseDescription),
		})
	}
	slices.SortFunc(out, func(a, b StaticLease) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), bytes.Compare(a.MAC, b.MAC))
	})
	return out
}

// leaseOptions packs a lease the way GetDhcpStaticLeases returns it.
func leaseOptions(l StaticLease) (mac []byte, f map[string]any, err error) {
	if len(l.MAC) == 0 {
		return nil, nil, errors.New("lease needs a MAC address")
	}
	mac, err = rpc.PackMAC(l.MAC)
	if err != nil {
		return nil, nil, err
	}
	ip, err := rpc.PackAddr(l.IP)
	if err != nil {
		return nil, nil, err
	}
	f = map[string]any{
		leaseName:        l.Name,
		leaseMAC:         mac,
		leaseAddress:     ip,
		leaseDescription: l.Description,
	}
	return mac, f, nil
}

// AddStaticLease reserves l.IP for l.MAC.
func AddStaticLease(ctx context.Context, c *amplifi.Client, l StaticLease) error {
	mac, f, err := leaseOptions(l)
	if err != nil {
		return fmt.Errorf("static lease: %w", err)
	}
	if err := setLeases(ctx, c, "AddDhcpStaticLeases", map[string]any{string(mac): f}); err != nil {
		return fmt.Errorf("add static lease %q: %w", l.MAC.String(), err)
	}
	return nil
}

// StaticLeaseUpdate lists the lease fields to change; nil fields are left
// as they are.
type StaticLeaseUpdate struct {
	IP          *netip.Addr
	Name        *string
	Description *string
}

func (u StaticLeaseUpdate) options() (map[string]any, error) {
	m := map[string]any{}
	if u.IP != nil {
		ip, err := rpc.PackAddr(*u.IP)
		if err != nil {
			return nil, err
		}
		m[leaseAddress] = ip
	}
	rpc.Put(m, leaseName, u.Name)
	rpc.Put(m, leaseDescription, u.Description)
	return m, nil
}

// UpdateStaticLease changes the lease for mac. It reads all leases, applies
// u to that one and writes them all back.
func UpdateStaticLease(ctx context.Context, c *amplifi.Client, mac amplifi.MAC, u StaticLeaseUpdate) error {
	changes, err := u.options()
	if err != nil {
		return fmt.Errorf("static lease %q: %w", mac.String(), err)
	}
	if len(changes) == 0 {
		return nil
	}

	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetDhcpStaticLeases", nil)
	if err != nil {
		return fmt.Errorf("DHCP static leases: %w", err)
	}
	f, ok := m[string(mac)].(map[string]any)
	if !ok {
		return fmt.Errorf("no static lease for %q", mac.String())
	}
	maps.Copy(f, changes)

	if err := setLeases(ctx, c, "SetDhcpStaticLeases", m); err != nil {
		return fmt.Errorf("static lease %q: %w", mac.String(), err)
	}
	return nil
}

// DeleteStaticLeases removes the leases for the given MACs.
func DeleteStaticLeases(ctx context.Context, c *amplifi.Client, macs ...amplifi.MAC) error {
	arg := make([]any, 0, len(macs))
	for _, mac := range macs {
		b, err := rpc.PackMAC(mac)
		if err != nil {
			return err
		}
		arg = append(arg, b)
	}
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "DeleteDhcpStaticLeases", arg); err != nil {
		return fmt.Errorf("delete static leases: %w", err)
	}
	return nil
}

// setLeases sends a lease map to method and checks the result.
func setLeases(ctx context.Context, c *amplifi.Client, method string, leases map[string]any) error {
	packed, err := packLeases(leases)
	if err != nil {
		return err
	}
	return rpc.Set(ctx, c, amplifi.IfaceDevice, method, amplifi.RawMsgpack(packed))
}

// packLeases encodes a lease map keyed by raw MAC bytes. The keys must be
// msgpack binary, which wire.EncodeMsgpack cannot express, so the map is
// assembled here with each value encoded by wire.EncodeMsgpack.
func packLeases(leases map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	if err := enc.EncodeMapLen(len(leases)); err != nil {
		return nil, fmt.Errorf("encode leases: %w", err)
	}
	for _, key := range slices.Sorted(maps.Keys(leases)) {
		if err := enc.EncodeBytes([]byte(key)); err != nil {
			return nil, fmt.Errorf("encode leases: %w", err)
		}
		v, err := wire.EncodeMsgpack(leases[key])
		if err != nil {
			return nil, err
		}
		if err := enc.Encode(msgpack.RawMessage(v)); err != nil {
			return nil, fmt.Errorf("encode leases: %w", err)
		}
	}
	return buf.Bytes(), nil
}
