package network

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
	"github.com/borud/amplifi/internal/wire"
	"github.com/vmihailenco/msgpack/v5"
)

// DhcpPoolConfigOption codes.
const (
	dhcpPoolInterface = "1"
	dhcpPoolIgnore    = "2"
	dhcpPoolOffset    = "3"
	dhcpPoolLength    = "4"
	dhcpPoolLeaseTime = "5"
	dhcpPoolDHCP4Mode = "6"
	dhcpPoolDHCP6Mode = "7"
	dhcpPoolRAMode    = "8"
	dhcpPoolNDPMode   = "9"
)

// DhcpStaticLeaseConfigOption codes.
const (
	dhcpLeaseName        = "1"
	dhcpLeaseMAC         = "2"
	dhcpLeaseAddress     = "3"
	dhcpLeaseDescription = "4"
)

// dhcpModeNames maps DhcpMode codes.
var dhcpModeNames = map[string]string{"1": "disabled", "2": "server", "3": "relay", "4": "hybrid"}

func dhcpModeCode(s string) (int64, error) {
	return rpc.Code("DHCP mode", dhcpModeNames, s)
}

func dhcpMode(m map[string]any, key string) string {
	n := rpc.Int(m, key)
	if n <= 0 {
		return ""
	}
	return rpc.CodeName(dhcpModeNames, n)
}

// DHCPPool is the DHCP server configuration of one network (GetDhcpPools).
// Addresses are handed out from Offset to Offset+Length-1 within the
// network's subnet.
type DHCPPool struct {
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

// DHCPPools lists the DHCP pools, sorted by name.
func DHCPPools(ctx context.Context, c *amplifi.Client) ([]DHCPPool, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetDhcpPools", nil)
	if err != nil {
		return nil, fmt.Errorf("DHCP pools: %w", err)
	}
	return parseDHCPPools(m), nil
}

// parseDHCPPools decodes DhcpPools: a map of pool name to
// DhcpPoolConfigOption map.
func parseDHCPPools(m map[string]any) []DHCPPool {
	out := make([]DHCPPool, 0, len(m))
	for _, poolName := range slices.Sorted(maps.Keys(m)) {
		f, ok := m[poolName].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, DHCPPool{
			Name:      poolName,
			Interface: rpc.String(f, dhcpPoolInterface),
			Ignore:    rpc.Bool(f, dhcpPoolIgnore),
			Offset:    int(rpc.Int(f, dhcpPoolOffset)),
			Length:    int(rpc.Int(f, dhcpPoolLength)),
			LeaseTime: int(rpc.Int(f, dhcpPoolLeaseTime)),
			DHCP4Mode: dhcpMode(f, dhcpPoolDHCP4Mode),
			DHCP6Mode: dhcpMode(f, dhcpPoolDHCP6Mode),
			RAMode:    dhcpMode(f, dhcpPoolRAMode),
			NDPMode:   dhcpMode(f, dhcpPoolNDPMode),
		})
	}
	return out
}

// DHCPPoolUpdate lists the pool settings to change; nil fields are left as
// they are. Modes are "disabled", "server", "relay" or "hybrid". The unit of
// LeaseTime is probably seconds; it is untested.
type DHCPPoolUpdate struct {
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

func (u DHCPPoolUpdate) options() (map[string]any, error) {
	o := optionSet{m: map[string]any{}}
	rpc.Put(o.m, dhcpPoolInterface, u.Interface)
	rpc.Put(o.m, dhcpPoolIgnore, u.Ignore)
	rpc.PutInt(o.m, dhcpPoolOffset, u.Offset)
	rpc.PutInt(o.m, dhcpPoolLength, u.Length)
	rpc.PutInt(o.m, dhcpPoolLeaseTime, u.LeaseTime)
	for key, mode := range map[string]*string{
		dhcpPoolDHCP4Mode: u.DHCP4Mode,
		dhcpPoolDHCP6Mode: u.DHCP6Mode,
		dhcpPoolRAMode:    u.RAMode,
		dhcpPoolNDPMode:   u.NDPMode,
	} {
		if mode == nil {
			continue
		}
		code, err := dhcpModeCode(*mode)
		o.set(key, code, err)
	}
	return o.m, o.err
}

// AddDHCPPool adds a pool named poolName (normally the network interface
// name, such as "lan"). The interface option defaults to poolName.
func AddDHCPPool(ctx context.Context, c *amplifi.Client, poolName string, u DHCPPoolUpdate) error {
	if u.Interface == nil {
		u.Interface = &poolName
	}
	f, err := u.options()
	if err != nil {
		return fmt.Errorf("DHCP pool %q: %w", poolName, err)
	}

	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "AddDhcpPools", map[string]any{poolName: f}); err != nil {
		return fmt.Errorf("add DHCP pool %q: %w", poolName, err)
	}
	return nil
}

// UpdateDHCPPool changes one pool. It reads all pools, applies u to the
// named one and writes them all back.
func UpdateDHCPPool(ctx context.Context, c *amplifi.Client, poolName string, u DHCPPoolUpdate) error {
	return updateDHCPPool(ctx, c, poolName, u)
}

func updateDHCPPool(ctx context.Context, c rpc.Caller, poolName string, u DHCPPoolUpdate) error {
	changes, err := u.options()
	if err != nil {
		return fmt.Errorf("DHCP pool %q: %w", poolName, err)
	}
	if err := rpc.UpdateEntry(ctx, c, amplifi.IfaceDevice, "GetDhcpPools", nil, "SetDhcpPools", poolName, changes); err != nil {
		return fmt.Errorf("DHCP pool %q: %w", poolName, err)
	}
	return nil
}

// DeleteDHCPPools deletes pools by name. The argument is an array of names.
func DeleteDHCPPools(ctx context.Context, c *amplifi.Client, names ...string) error {
	arg := make([]any, 0, len(names))
	for _, n := range names {
		arg = append(arg, n)
	}
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "DeleteDhcpPools", arg); err != nil {
		return fmt.Errorf("delete DHCP pools: %w", err)
	}
	return nil
}

// DHCPStaticLease is a fixed address reservation (DhcpStaticLease).
type DHCPStaticLease struct {
	MAC         amplifi.MAC `json:"mac"`
	IP          string      `json:"ip"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
}

// DHCPStaticLeases lists the static leases, sorted by name then MAC.
func DHCPStaticLeases(ctx context.Context, c *amplifi.Client) ([]DHCPStaticLease, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetDhcpStaticLeases", nil)
	if err != nil {
		return nil, fmt.Errorf("DHCP static leases: %w", err)
	}
	return parseDHCPStaticLeases(m), nil
}

// parseDHCPStaticLeases decodes DhcpStaticLeases: a map keyed by binary MAC
// of DhcpStaticLeaseConfigOption maps.
func parseDHCPStaticLeases(m map[string]any) []DHCPStaticLease {
	out := make([]DHCPStaticLease, 0, len(m))
	for key, v := range m {
		f, ok := v.(map[string]any)
		mac := rpc.MAC(key)
		if !ok || mac == nil {
			continue
		}
		out = append(out, DHCPStaticLease{
			MAC:         mac,
			IP:          formatAddr(f[dhcpLeaseAddress]),
			Name:        rpc.String(f, dhcpLeaseName),
			Description: rpc.String(f, dhcpLeaseDescription),
		})
	}
	slices.SortFunc(out, func(a, b DHCPStaticLease) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), bytes.Compare(a.MAC, b.MAC))
	})
	return out
}

// dhcpLeaseOptions packs a lease the way GetDhcpStaticLeases returns it.
func dhcpLeaseOptions(l DHCPStaticLease) (mac []byte, f map[string]any, err error) {
	if len(l.MAC) == 0 {
		return nil, nil, errors.New("lease needs a MAC address")
	}
	mac, err = packMAC(l.MAC)
	if err != nil {
		return nil, nil, err
	}
	ip, err := packIP(l.IP)
	if err != nil {
		return nil, nil, err
	}
	f = map[string]any{
		dhcpLeaseName:        l.Name,
		dhcpLeaseMAC:         mac,
		dhcpLeaseAddress:     ip,
		dhcpLeaseDescription: l.Description,
	}
	return mac, f, nil
}

// AddDHCPStaticLease reserves l.IP for l.MAC.
func AddDHCPStaticLease(ctx context.Context, c *amplifi.Client, l DHCPStaticLease) error {
	mac, f, err := dhcpLeaseOptions(l)
	if err != nil {
		return fmt.Errorf("static lease: %w", err)
	}
	if err := dhcpSetLeases(ctx, c, "AddDhcpStaticLeases", map[string]any{string(mac): f}); err != nil {
		return fmt.Errorf("add static lease %q: %w", l.MAC.String(), err)
	}
	return nil
}

// DHCPStaticLeaseUpdate lists the lease fields to change; nil fields are
// left as they are.
type DHCPStaticLeaseUpdate struct {
	IP          *string
	Name        *string
	Description *string
}

// UpdateDHCPStaticLease changes the lease for mac. It reads all leases,
// applies u to that one and writes them all back.
func UpdateDHCPStaticLease(ctx context.Context, c *amplifi.Client, mac amplifi.MAC, u DHCPStaticLeaseUpdate) error {
	o := optionSet{m: map[string]any{}}
	o.ip(dhcpLeaseAddress, u.IP)
	rpc.Put(o.m, dhcpLeaseName, u.Name)
	rpc.Put(o.m, dhcpLeaseDescription, u.Description)
	if o.err != nil {
		return fmt.Errorf("static lease %q: %w", mac.String(), o.err)
	}
	if len(o.m) == 0 {
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
	maps.Copy(f, o.m)

	if err := dhcpSetLeases(ctx, c, "SetDhcpStaticLeases", m); err != nil {
		return fmt.Errorf("static lease %q: %w", mac.String(), err)
	}
	return nil
}

// DeleteDHCPStaticLeases removes the leases for the given MACs. The argument
// is an array of binary MACs.
func DeleteDHCPStaticLeases(ctx context.Context, c *amplifi.Client, macs ...amplifi.MAC) error {
	arg := make([]any, 0, len(macs))
	for _, mac := range macs {
		b, err := packMAC(mac)
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

// dhcpSetLeases sends a lease map to method and checks the result.
func dhcpSetLeases(ctx context.Context, c *amplifi.Client, method string, leases map[string]any) error {
	packed, err := dhcpPackLeases(leases)
	if err != nil {
		return err
	}
	return rpc.Set(ctx, c, amplifi.IfaceDevice, method, amplifi.RawMsgpack(packed))
}

// dhcpPackLeases encodes a lease map keyed by raw MAC bytes. The keys must
// be msgpack binary, which wire.EncodeMsgpack cannot express, so the map is
// assembled here with each value encoded by wire.EncodeMsgpack.
func dhcpPackLeases(leases map[string]any) ([]byte, error) {
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
