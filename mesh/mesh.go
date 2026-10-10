// Package mesh covers the units that make up the mesh: the connected mesh
// points, every registered unit, backhaul signal quality, the Ethernet ports
// of every unit, factory pairing of new mesh points, and ignoring or factory
// resetting units.
package mesh

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
	"github.com/borud/amplifi/internal/wire"
)

// Peer is a mesh point as seen by the router.
type Peer struct {
	MAC  amplifi.MAC `json:"mac"`
	Name string      `json:"name"`

	Role amplifi.Role `json:"role"`
	IP   netip.Addr   `json:"ip"`

	// Uptime marshals as whole seconds.
	Uptime time.Duration `json:"uptime"`

	// Band and RSSI (dBm) describe the backhaul link.
	Band   string      `json:"band"`
	RSSI   int         `json:"rssi"`
	Master amplifi.MAC `json:"master,omitempty"`
}

// MarshalJSON renders the uptime in seconds.
func (p Peer) MarshalJSON() ([]byte, error) {
	type peer Peer
	return json.Marshal(struct {
		peer
		Uptime int64 `json:"uptime"`
	}{peer(p), int64(p.Uptime / time.Second)})
}

// Peers lists the mesh points connected to the router (the router itself is
// not included), the router role first and the rest by name.
func Peers(ctx context.Context, c *amplifi.Client) ([]Peer, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceClusterNode, "GetPeerList", nil)
	if err != nil {
		return nil, fmt.Errorf("mesh peers: %w", err)
	}

	peers := make([]Peer, 0, len(m))
	for mac, v := range m {
		fields, ok := v.([]any)
		if !ok || len(fields) < 2 {
			continue
		}
		peers = append(peers, parsePeer(mac, fields))
	}

	slices.SortFunc(peers, func(a, b Peer) int { return compareUnits(a.Role, a.Name, b.Role, b.Name) })
	return peers, nil
}

// Band and RSSI (dBm) assumed for a peer entry that omits them.
const (
	defaultPeerBand = "5GHz"
	defaultPeerRSSI = -50
)

// parsePeer decodes one GetPeerList entry. Each is an array of
// [name, role, ip, uptime, flags, band, rssi, master MAC], keyed by the
// peer's binary MAC. Trailing fields may be missing.
func parsePeer(mac string, f []any) Peer {
	at := func(i int) any {
		if i < len(f) {
			return f[i]
		}
		return nil
	}

	p := Peer{MAC: rpc.MAC(mac), Band: defaultPeerBand, RSSI: defaultPeerRSSI}
	p.Name, _ = at(0).(string)
	if role, ok := wire.ToInt64(at(1)); ok {
		p.Role = rpc.Role(role)
	}
	if ip, ok := at(2).([]byte); ok {
		a, _ := netip.AddrFromSlice(ip)
		p.IP = a.Unmap()
	}
	if up, ok := wire.ToInt64(at(3)); ok {
		p.Uptime = time.Duration(up) * time.Second
	}
	if band, ok := wire.ToInt64(at(5)); ok {
		p.Band = rpc.CodeName(rpc.BandNames, band)
	}
	if rssi, ok := wire.ToInt64(at(6)); ok {
		p.RSSI = int(rssi)
	}
	p.Master = rpc.MAC(at(7))
	return p
}

// compareUnits orders units with the router first and the rest by name.
func compareUnits(roleA amplifi.Role, nameA string, roleB amplifi.Role, nameB string) int {
	routerA, routerB := roleA == amplifi.RoleRouter, roleB == amplifi.RoleRouter
	if routerA != routerB {
		if routerA {
			return -1
		}
		return 1
	}
	return cmp.Compare(nameA, nameB)
}

// Unit is a unit registered in the mesh, whether or not it is currently
// connected.
type Unit struct {
	MAC      amplifi.MAC  `json:"mac"`
	Name     string       `json:"name"`
	Role     amplifi.Role `json:"role"`
	Platform string       `json:"platform,omitempty"`
}

// Units lists every unit registered in the mesh, the router first and the
// rest by name.
func Units(ctx context.Context, c *amplifi.Client) ([]Unit, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceClusterNode, "GetRegisteredNodeList", nil)
	if err != nil {
		return nil, fmt.Errorf("mesh units: %w", err)
	}
	return parseUnits(m), nil
}

// parseUnits decodes a GetRegisteredNodeList result: a map of binary MAC to
// [name, role, platform name]. Entries without a
// name and role are skipped.
func parseUnits(m map[string]any) []Unit {
	units := make([]Unit, 0, len(m))
	for mac, v := range m {
		f, ok := v.([]any)
		if !ok || len(f) < 2 {
			continue
		}
		name, ok := f[0].(string)
		if !ok {
			continue
		}
		role, ok := wire.ToInt64(f[1])
		if !ok {
			continue
		}

		u := Unit{MAC: rpc.MAC(mac), Name: name, Role: rpc.Role(role)}
		if len(f) > 2 {
			u.Platform, _ = f[2].(string)
		}
		units = append(units, u)
	}

	slices.SortFunc(units, func(a, b Unit) int { return compareUnits(a.Role, a.Name, b.Role, b.Name) })
	return units
}

// Signal is the backhaul signal quality of one mesh peer as seen on one band
// and network.
type Signal struct {
	Band    string      `json:"band"`
	Network string      `json:"network"`
	MAC     amplifi.MAC `json:"mac"`
	Quality int         `json:"quality"`
}

// Signals reports the signal quality of the mesh peers' wireless links,
// sorted by band, network and MAC. Not every firmware supports it.
func Signals(ctx context.Context, c *amplifi.Client) ([]Signal, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceClusterNode, "GetPeerWifiSignalQuality", nil)
	if err != nil {
		return nil, fmt.Errorf("mesh signal quality: %w", err)
	}
	return parseSignals(m), nil
}

// parseSignals decodes a WifiClientBandMap: band code -> interface role code
// -> binary MAC -> signal quality.
func parseSignals(m map[string]any) []Signal {
	out := []Signal{}
	for band, roles := range rpc.Submaps(m) {
		for role, peers := range rpc.Submaps(roles) {
			for mac := range peers {
				out = append(out, Signal{
					Band:    rpc.KeyName(rpc.BandNames, band),
					Network: rpc.KeyName(rpc.NetworkNames, role),
					MAC:     rpc.MAC(mac),
					Quality: int(rpc.Int(peers, mac)),
				})
			}
		}
	}

	slices.SortFunc(out, func(a, b Signal) int {
		return cmp.Or(cmp.Compare(a.Band, b.Band), cmp.Compare(a.Network, b.Network), bytes.Compare(a.MAC, b.MAC))
	})
	return out
}

// Port is the state of one Ethernet port on one unit.
type Port struct {
	Unit          amplifi.MAC `json:"unit"`
	Port          string      `json:"port"`
	Link          bool        `json:"link"`
	LinkSpeedMbps int         `json:"link_speed_mbps"`
	RxKbps        int         `json:"rx_kbps"`
	TxKbps        int         `json:"tx_kbps"`
}

// portInfo is one port in a GetEthernetPortInfo result.
type portInfo struct {
	Link      bool `json:"link"`
	LinkSpeed int  `json:"link_speed"`
	RxBitrate int  `json:"rx_bitrate"`
	TxBitrate int  `json:"tx_bitrate"`
}

// portSignalWait bounds the wait for an EthernetPortInfoChanged signal.
const portSignalWait = 3 * time.Second

// Ports reports the Ethernet ports of every unit in the mesh, sorted by unit
// and port.
//
// GetEthernetPortInfo only has data while the router's port polling is on;
// otherwise it is empty. The router's
// EthernetPortInfoChanged signal carries the same data, so Ports then waits
// briefly for one. If none arrives the result is empty and the session is
// unusable afterwards.
func Ports(ctx context.Context, c *amplifi.Client) ([]Port, error) {
	var info map[string]map[string]portInfo
	if err := rpc.Value(ctx, c, amplifi.IfaceClusterNode, "GetEthernetPortInfo", nil, &info); err != nil {
		return nil, fmt.Errorf("ethernet ports: %w", err)
	}
	if len(info) == 0 {
		return portsFromSignal(ctx, c)
	}
	return flattenPorts(info)
}

// portsFromSignal waits for an EthernetPortInfoChanged signal and decodes its
// payload, which has the GetEthernetPortInfo shape.
func portsFromSignal(ctx context.Context, c *amplifi.Client) ([]Port, error) {
	ctx, cancel := context.WithTimeout(ctx, portSignalWait)
	defer cancel()

	for sig, err := range c.Events(ctx) {
		if err != nil {
			return nil, fmt.Errorf("ethernet ports: %w", err)
		}
		if sig.Name != "EthernetPortInfoChanged" {
			continue
		}

		var info map[string]map[string]portInfo
		if err := rpc.Decode(sig.Name, sig.Payload, &info); err != nil {
			return nil, fmt.Errorf("ethernet ports: %w", err)
		}
		return flattenPorts(info)
	}
	return []Port{}, nil
}

// flattenPorts turns the unit MAC -> port -> info map of GetEthernetPortInfo
// into a sorted list. The router keys units by their
// MAC as text.
func flattenPorts(info map[string]map[string]portInfo) ([]Port, error) {
	ports := []Port{}
	for unit, unitPorts := range info {
		mac, err := amplifi.ParseMAC(unit)
		if err != nil {
			return nil, fmt.Errorf("ethernet ports: unit: %w", err)
		}
		for port, p := range unitPorts {
			ports = append(ports, Port{
				Unit:          mac,
				Port:          port,
				Link:          p.Link,
				LinkSpeedMbps: p.LinkSpeed,
				RxKbps:        p.RxBitrate,
				TxKbps:        p.TxBitrate,
			})
		}
	}

	slices.SortFunc(ports, func(a, b Port) int {
		return cmp.Or(bytes.Compare(a.Unit, b.Unit), rpc.CompareNumeric(a.Port, b.Port))
	})
	return ports, nil
}

// Ignore tells the router to ignore the given units, so they are no longer
// offered for adoption into the mesh. The argument, a msgpack array of binary
// MACs as for FactoryReset, is untested.
func Ignore(ctx context.Context, c *amplifi.Client, macs ...amplifi.MAC) error {
	if err := rpc.Set(ctx, c, amplifi.IfaceClusterNode, "IgnoreNodes", macArray(macs)); err != nil {
		return fmt.Errorf("ignore mesh units: %w", err)
	}
	return nil
}

// ResettableUnits lists the units the router is able to factory reset. The
// result format is unknown; binary MACs found as array elements or map keys
// are returned.
func ResettableUnits(ctx context.Context, c *amplifi.Client) ([]amplifi.MAC, error) {
	v, err := c.Call(ctx, amplifi.IfaceClusterNode, "GetFactoryResetNodeList", nil)
	if err != nil {
		return nil, fmt.Errorf("resettable units: %w", err)
	}
	return collectMACs(v)
}

// collectMACs extracts 6 byte MACs from an array of binaries or a map keyed
// by binary MACs. A nil result is an empty list.
func collectMACs(v any) ([]amplifi.MAC, error) {
	macs := []amplifi.MAC{}
	switch t := v.(type) {
	case nil:
		return macs, nil
	case []any:
		for _, e := range t {
			if _, binary := e.([]byte); !binary {
				continue
			}
			if mac := rpc.MAC(e); mac != nil {
				macs = append(macs, mac)
			}
		}
	case map[string]any:
		for k := range t {
			if mac := rpc.MAC(k); mac != nil {
				macs = append(macs, mac)
			}
		}
	default:
		return nil, fmt.Errorf("resettable units: unexpected result %T", v)
	}

	slices.SortFunc(macs, func(a, b amplifi.MAC) int { return bytes.Compare(a, b) })
	return macs, nil
}

// FactoryReset factory resets one mesh point (not the unit this session is
// connected to). It is irreversible.
func FactoryReset(ctx context.Context, c *amplifi.Client, mac amplifi.MAC) error {
	if err := rpc.Set(ctx, c, amplifi.IfaceClusterNode, "FactoryResetNodes", macArray([]amplifi.MAC{mac})); err != nil {
		return fmt.Errorf("factory reset %q: %w", mac, err)
	}
	return nil
}

// macArray packs MACs as a msgpack array of binaries.
func macArray(macs []amplifi.MAC) []any {
	a := make([]any, 0, len(macs))
	for _, m := range macs {
		a = append(a, []byte(m))
	}
	return a
}
