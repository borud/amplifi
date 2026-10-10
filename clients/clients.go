// Package clients covers the clients on the network: the mesh's current and
// historic client lists, Wi-Fi association details and signal quality,
// client counts, custom client settings (name, pause), fingerprints and the
// guest Wi-Fi whitelist.
package clients

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
	"github.com/borud/amplifi/internal/wire"
)

// Option codes of a GetWifiClientInfo client entry. Codes 7-14 are byte counts
// over 5 to 60 second windows and are left out.
const (
	wifiMode        = "2"
	wifiSignal      = "3"
	wifiInactive    = "4"
	wifiRxBytes     = "5"
	wifiTxBytes     = "6"
	wifiIP          = "15"
	wifiLease       = "16"
	wifiHostName    = "17"
	wifiDescription = "18"
	wifiRxBitrate   = "19"
	wifiTxBitrate   = "20"
	wifiRxMCS       = "21"
	wifiTxMCS       = "22"
	wifiRxMHz       = "23"
	wifiTxMHz       = "24"
	wifiStreams     = "25"
	wifiBandwidth   = "26"
	wifiHappiness   = "27"
	wifiRadioMode   = "28"
)

// modeNames maps WifiMode codes.
var modeNames = map[string]string{
	"1": "802.11a", "2": "802.11b", "3": "802.11g", "4": "802.11n", "5": "802.11ac", "6": "802.11ax",
}

// Client is a Wi-Fi or wired client, merged from the mesh's client list
// (ClusterNode.GetClientInfo) and the Wi-Fi association tables. Fields holds
// the client list entry as the router sent it.
type Client struct {
	MAC amplifi.MAC `json:"mac"`

	// Name is Description, else HostName, else the MAC.
	Name string `json:"name"`

	// Description is the name set by the user.
	Description string     `json:"description,omitempty"`
	HostName    string     `json:"host_name,omitempty"`
	IP          netip.Addr `json:"ip,omitzero"`

	// Connection is "wired", "2.4GHz", "5GHz", "wifi" or the router's value.
	Connection string `json:"connection,omitempty"`

	// Port is the Ethernet port of a wired client.
	Port *int `json:"port,omitempty"`

	// Network is the Wi-Fi network: main, guest, iot, ...
	Network string `json:"network,omitempty"`

	// Unit is the mesh point the client is attached to; nil for the router.
	Unit     amplifi.MAC `json:"unit,omitempty"`
	UnitName string      `json:"unit_name,omitempty"`

	// IconID is the fingerprint device ID.
	IconID int  `json:"icon_id,omitempty"`
	Online bool `json:"online"`

	// LastSeen and TimesSeen are only set in History.
	LastSeen  time.Time `json:"last_seen,omitzero"`
	TimesSeen int       `json:"times_seen,omitempty"`

	WiFi   *Association   `json:"wifi,omitempty"`
	Custom map[string]any `json:"custom,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

// Association is one client's Wi-Fi association as reported by
// GetWifiClientInfo. Signal is the router's signal quality figure; its scale
// is unknown. RxBitrate, TxBitrate, Inactive and LeaseValidity are the
// router's raw values and their units are unconfirmed; bitrates such as
// 866700 for an 866.7 Mbit/s link suggest kbit/s.
type Association struct {
	MAC            amplifi.MAC `json:"mac"`
	Unit           amplifi.MAC `json:"unit,omitempty"`
	Band           string      `json:"band"`
	Network        string      `json:"network"`
	Mode           string      `json:"mode,omitempty"`
	RadioMode      string      `json:"radio_mode,omitempty"`
	Signal         int         `json:"signal"`
	Inactive       int         `json:"inactive"`
	RxBytes        int64       `json:"rx_bytes"`
	TxBytes        int64       `json:"tx_bytes"`
	RxBitrate      int         `json:"rx_bitrate"`
	TxBitrate      int         `json:"tx_bitrate"`
	RxMCS          int         `json:"rx_mcs"`
	TxMCS          int         `json:"tx_mcs"`
	RxMHz          int         `json:"rx_mhz"`
	TxMHz          int         `json:"tx_mhz"`
	SpatialStreams int         `json:"spatial_streams"`
	MaxBandwidth   int         `json:"max_bandwidth"`
	Happiness      int         `json:"happiness"`
	IP             netip.Addr  `json:"ip,omitzero"`
	LeaseValidity  int         `json:"lease_validity"`
	HostName       string      `json:"host_name,omitempty"`
	Description    string      `json:"description,omitempty"`
}

// Signal is one Wi-Fi client's signal quality.
type Signal struct {
	MAC      amplifi.MAC `json:"mac"`
	Unit     amplifi.MAC `json:"unit,omitempty"`
	Band     string      `json:"band"`
	Network  string      `json:"network"`
	UnitName string      `json:"unit_name,omitempty"`
	Quality  int         `json:"quality"`
}

// Counts is the number of Wi-Fi clients by band and by network.
type Counts struct {
	ByBand    map[string]int `json:"by_band"`
	ByNetwork map[string]int `json:"by_network"`
	Total     int            `json:"total"`
}

// List lists the clients known to the mesh (ClusterNode.GetClientInfo),
// enriched with custom info, Wi-Fi details and unit names when the router
// provides them. Wi-Fi clients missing from the client list are included.
// The result is sorted by name, then MAC.
func List(ctx context.Context, c *amplifi.Client) ([]Client, error) {
	info, err := clientInfo(ctx, c, "GetClientInfo")
	if err != nil {
		return nil, fmt.Errorf("client list: %w", err)
	}

	custom, err := customInfo(ctx, c)
	if err != nil && !amplifi.IsRouterError(err) {
		return nil, fmt.Errorf("client list: %w", err)
	}
	assocs, err := Associations(ctx, c)
	if err != nil && !amplifi.IsRouterError(err) {
		return nil, fmt.Errorf("client list: %w", err)
	}
	units, err := unitNames(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("client list: %w", err)
	}

	return current(info, custom, assocs, units), nil
}

// History lists clients the mesh has seen before
// (ClusterNode.GetHistoricClientInfo), sorted by name, then MAC. Clients in
// the current client list are marked online. History entries do not say
// which unit a client used, so Unit and UnitName stay empty.
func History(ctx context.Context, c *amplifi.Client) ([]Client, error) {
	info, err := clientInfo(ctx, c, "GetHistoricClientInfo")
	if err != nil {
		return nil, fmt.Errorf("client history: %w", err)
	}

	online, err := clientInfo(ctx, c, "GetClientInfo")
	if err != nil && !amplifi.IsRouterError(err) {
		return nil, fmt.Errorf("client history: %w", err)
	}
	return history(info, online), nil
}

// Associations lists the Wi-Fi associations on every network and band. It
// asks ClusterNode.GetWifiClientInfo, which covers the whole mesh and names
// each client's unit, and falls back to Device.GetWifiClientInfo (this unit
// only) if the unit refuses.
func Associations(ctx context.Context, c *amplifi.Client) ([]Association, error) {
	// [nil, nil] asks for every network on every band.
	m, err := meshOrDevice(ctx, c, "GetWifiClientInfo", rpc.TwoNulls)
	if err != nil {
		return nil, fmt.Errorf("wifi clients: %w", err)
	}
	return parseAssociations(m), nil
}

// Signals reports the signal quality of every Wi-Fi client. It asks
// ClusterNode.GetWifiClientSignalQuality and falls back to the Device variant
// (this unit only) if the unit refuses.
func Signals(ctx context.Context, c *amplifi.Client) ([]Signal, error) {
	m, err := meshOrDevice(ctx, c, "GetWifiClientSignalQuality", rpc.Null)
	if err != nil {
		return nil, fmt.Errorf("client signals: %w", err)
	}

	units, err := unitNames(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("client signals: %w", err)
	}
	signals := parseSignals(m)
	for i, s := range signals {
		signals[i].UnitName = unitName(units, s.Unit)
	}
	return signals, nil
}

// Count counts the Wi-Fi clients connected to the unit, leaving out mesh
// backhaul and setup networks.
func Count(ctx context.Context, c *amplifi.Client) (*Counts, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetWifiClientCount", rpc.Null)
	if err != nil {
		return nil, fmt.Errorf("client count: %w", err)
	}

	wc := &Counts{ByBand: map[string]int{}, ByNetwork: map[string]int{}}
	for _, p := range descend([]path{{v: m}}, 2) {
		band, role := p.keys[0], p.keys[1]
		if !rpc.ClientNetworks[role] {
			continue
		}
		n, _ := wire.ToInt64(p.v)
		wc.ByBand[rpc.KeyName(rpc.BandNames, band)] += int(n)
		wc.ByNetwork[rpc.KeyName(rpc.NetworkNames, role)] += int(n)
		wc.Total += int(n)
	}
	return wc, nil
}

// customInfo returns the custom settings saved for clients
// (ClusterNode.GetClientCustomInfo), keyed by lowercase MAC.
func customInfo(ctx context.Context, c *amplifi.Client) (map[string]map[string]any, error) {
	v, err := c.CallJSON(ctx, amplifi.IfaceClusterNode, "GetClientCustomInfo", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("client custom info: %w", err)
	}
	return entryMap("GetClientCustomInfo", v)
}

// setCustomInfo stores custom settings for one client
// (ClusterNode.SetClientCustomInfo with {mac: entry}).
func setCustomInfo(ctx context.Context, c *amplifi.Client, mac amplifi.MAC, entry map[string]any) error {
	v, err := c.CallJSON(ctx, amplifi.IfaceClusterNode, "SetClientCustomInfo", map[string]any{mac.String(): entry})
	if err != nil {
		return fmt.Errorf("set client custom info: %w", err)
	}
	return rpc.ExpectOK("SetClientCustomInfo", v)
}

// Rename sets the name shown for a client. The router reports that name as
// "description" in GetClientInfo, so this sends {"description": name}
// through SetClientCustomInfo. This is untested.
func Rename(ctx context.Context, c *amplifi.Client, mac amplifi.MAC, name string) error {
	return setCustomInfo(ctx, c, mac, map[string]any{"description": name})
}

// SetPaused blocks (paused true) or restores a client's internet access
// (ClusterNode.SetWanClientPause).
func SetPaused(ctx context.Context, c *amplifi.Client, mac amplifi.MAC, paused bool) error {
	v, err := c.CallJSON(ctx, amplifi.IfaceClusterNode, "SetWanClientPause", map[string]any{
		"mac":   mac.String(),
		"pause": paused,
	})
	if err != nil {
		return fmt.Errorf("pause client: %w", err)
	}
	return rpc.ExpectOK("SetWanClientPause", v)
}

// Fingerprints returns the router's identification of each client
// (ClusterNode.fingerprint GetFingerprint), keyed by MAC as the router sends
// it.
func Fingerprints(ctx context.Context, c *amplifi.Client) (map[string]map[string]any, error) {
	v, err := c.Call(ctx, wire.IfaceFingerprint, "GetFingerprint", nil)
	if err != nil {
		return nil, fmt.Errorf("fingerprints: %w", err)
	}
	return entryMap("GetFingerprint", v)
}

// GuestWhitelist returns the guest Wi-Fi whitelist
// (ClusterNode.GetGuestWifiWhitelist) as the router sends it.
func GuestWhitelist(ctx context.Context, c *amplifi.Client) ([]any, error) {
	v, err := c.Call(ctx, amplifi.IfaceClusterNode, "GetGuestWifiWhitelist", nil)
	if err != nil {
		return nil, fmt.Errorf("guest whitelist: %w", err)
	}
	switch t := jsonValue(v).(type) {
	case nil:
		return []any{}, nil
	case []any:
		return t, nil
	default:
		return nil, fmt.Errorf("GetGuestWifiWhitelist: expected a list, got %T", t)
	}
}

// meshOrDevice calls method on ClusterNode (the whole mesh) and, if the unit
// refuses, on Device (this unit only). Firmware versions differ in which of
// the two they answer. Both answer with a map.
func meshOrDevice(ctx context.Context, c *amplifi.Client, method string, arg any) (map[string]any, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceClusterNode, method, arg)
	if amplifi.IsRouterError(err) {
		return rpc.Map(ctx, c, amplifi.IfaceDevice, method, arg)
	}
	return m, err
}

// unitNames names the mesh points by MAC from ClusterNode.GetPeerList, whose
// entries are [name, role, ...] keyed by binary MAC. The router is not among
// the peers; its name is stored under the empty key. Both lookups are best
// effort.
func unitNames(ctx context.Context, c *amplifi.Client) (map[string]string, error) {
	names := map[string]string{}

	peers, err := rpc.Map(ctx, c, amplifi.IfaceClusterNode, "GetPeerList", nil)
	if err != nil && !amplifi.IsRouterError(err) {
		return nil, err
	}
	for key, v := range peers {
		mac := rpc.MAC(key)
		fields, ok := v.([]any)
		if !ok || len(fields) == 0 || mac == nil {
			continue
		}
		names[mac.String()], _ = fields[0].(string)
	}

	v, err := c.Property(ctx, amplifi.IfaceDevice, "FriendlyName")
	if err != nil && !amplifi.IsRouterError(err) {
		return nil, err
	}
	names[""], _ = v.(string)
	return names, nil
}

// unitName names a mesh point: a peer by its name, anything else (including
// no MAC) as the router.
func unitName(units map[string]string, unit amplifi.MAC) string {
	if name, ok := units[unit.String()]; ok && len(unit) > 0 {
		return name
	}
	return units[""]
}

// clientInfo fetches a ClientInfo2List-shaped JSON value: an object of
// client entries keyed by MAC.
func clientInfo(ctx context.Context, c *amplifi.Client, method string) (map[string]map[string]any, error) {
	v, err := c.Call(ctx, amplifi.IfaceClusterNode, method, nil)
	if err != nil {
		return nil, err
	}
	return entryMap(method, v)
}

// jsonValue decodes a JSON answer that arrived as a string, in payload.value
// or as a msgpack str or bin; other values are returned as is.
func jsonValue(v any) any {
	if b, ok := v.([]byte); ok {
		v = string(b)
	}
	return rpc.JSONString(v)
}

// entryMap turns a JSON object of objects (keyed by MAC) into a map. An
// empty answer is an empty map; entries that are not objects are skipped.
func entryMap(method string, v any) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	v = jsonValue(v)
	if v == nil {
		return out, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected an object, got %T", method, v)
	}
	for k, e := range m {
		if entry, ok := e.(map[string]any); ok {
			out[k] = entry
		}
	}
	return out, nil
}

// history decodes history entries and marks those also present in the
// current client list (online) as online.
func history(info, online map[string]map[string]any) []Client {
	list := decodeInfo(info).list(nil, nil)
	for i, n := range list {
		_, list[i].Online = lookup(online, n.MAC.String())
	}
	return list
}

// current combines the current client list with custom info, Wi-Fi
// associations and unit names (see unitNames) into one sorted list. Every
// client list entry is kept and online; Wi-Fi data only adds to it, and
// Wi-Fi clients missing from the list are appended.
func current(info, custom map[string]map[string]any, assocs []Association, units map[string]string) []Client {
	set := decodeInfo(info)
	for _, nc := range set.byMAC {
		nc.Online = true
	}
	for _, a := range assocs {
		applyAssociation(set.add(a.MAC.String(), a.MAC), a, units)
	}
	return set.list(custom, units)
}

// clientSet is clients keyed by MAC, in the order they were added.
type clientSet struct {
	byMAC map[string]*Client
	order []string
}

// decodeInfo decodes the entries of a client list.
func decodeInfo(info map[string]map[string]any) *clientSet {
	set := &clientSet{byMAC: map[string]*Client{}, order: []string{}}
	for key, fields := range info {
		mac, err := amplifi.ParseMAC(key)
		if err == nil {
			key = mac.String()
		}
		applyFields(set.add(key, mac), fields)
	}
	return set
}

// add returns the client under key, adding it if it is new.
func (s *clientSet) add(key string, mac amplifi.MAC) *Client {
	if nc, ok := s.byMAC[key]; ok {
		return nc
	}
	nc := &Client{MAC: mac}
	s.byMAC[key] = nc
	s.order = append(s.order, key)
	return nc
}

// list returns the clients with custom info and unit names filled in, sorted
// by name, then MAC.
func (s *clientSet) list(custom map[string]map[string]any, units map[string]string) []Client {
	out := make([]Client, 0, len(s.order))
	for _, key := range s.order {
		nc := s.byMAC[key]
		if entry, ok := lookup(custom, key); ok && nc.Custom == nil {
			nc.Custom = entry
		}
		nc.UnitName = unitName(units, nc.Unit)
		nc.Name = cmp.Or(nc.Description, nc.HostName, key)
		out = append(out, *nc)
	}

	slices.SortFunc(out, func(a, b Client) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.MAC.String(), b.MAC.String()),
		)
	})
	return out
}

// applyAssociation adds a Wi-Fi association to nc. The client list's own
// values win, except that a generic "wifi" connection is narrowed to the
// band. The unit is only taken when it is a known mesh point.
func applyAssociation(nc *Client, a Association, units map[string]string) {
	nc.WiFi = &a
	nc.Online = true
	nc.Network = a.Network
	if nc.Connection == "" || nc.Connection == "wifi" {
		nc.Connection = a.Band
	}
	if nc.Unit == nil && len(a.Unit) > 0 && units[a.Unit.String()] != "" {
		nc.Unit = a.Unit
	}
	if !nc.IP.IsValid() {
		nc.IP = a.IP
	}
	if nc.HostName == "" {
		nc.HostName = a.HostName
	}
}

// lookup finds a MAC-keyed entry whatever the case of the key.
func lookup(m map[string]map[string]any, mac string) (map[string]any, bool) {
	for k, v := range m {
		if strings.EqualFold(k, mac) {
			return v, true
		}
	}
	return nil, false
}

// applyFields fills nc from a GetClientInfo entry, for example
// {"connection": "wireless", "peer": "00:00:5e:00:53:2e", "host_name": "cam1",
// "description": "Camera", "ip": "192.0.2.22", "icon_id": 3900,
// "custom_info": {...}} or {"connection": "ethernet", "port": 1}. "peer" is
// absent for clients attached to the router itself.
func applyFields(nc *Client, f map[string]any) {
	nc.Fields = f
	nc.Description, _ = f["description"].(string)
	nc.HostName, _ = f["host_name"].(string)
	if ip, ok := f["ip"].(string); ok {
		nc.IP, _ = netip.ParseAddr(ip)
	}
	if peer, ok := f["peer"].(string); ok {
		nc.Unit, _ = amplifi.ParseMAC(peer)
	}
	if id, ok := f["icon_id"].(float64); ok {
		nc.IconID = int(id)
	}
	if ts, ok := f["last_seen"].(float64); ok && ts > 0 {
		nc.LastSeen = time.Unix(int64(ts), 0)
	}
	if n, ok := f["times_seen"].(float64); ok {
		nc.TimesSeen = int(n)
	}
	if custom, ok := f["custom_info"].(map[string]any); ok {
		nc.Custom = custom
	}

	switch f["connection"] {
	case "ethernet":
		nc.Connection = "wired"
	case "wireless":
		nc.Connection = "wifi"
	default:
		nc.Connection, _ = f["connection"].(string)
	}
	if port, ok := f["port"].(float64); ok {
		p := int(port)
		nc.Port = &p
	}
}

// path is a value in nested maps with the keys leading to it.
type path struct {
	keys []string
	v    any
}

// descend goes levels deep into nested maps, returning every value at that
// depth. Values that are not maps before the last level are dropped.
func descend(paths []path, levels int) []path {
	for range levels {
		var next []path
		for _, p := range paths {
			m, ok := p.v.(map[string]any)
			if !ok {
				continue
			}
			for k, v := range m {
				next = append(next, path{keys: append(slices.Clip(p.keys), k), v: v})
			}
		}
		paths = next
	}
	return paths
}

// leaf is one client entry of a per-client Wi-Fi result.
type leaf struct {
	unit, mac     amplifi.MAC
	band, network string
	v             any
}

// leaves flattens a GetWifiClientInfo or GetWifiClientSignalQuality result:
// unit MAC -> band -> network -> client MAC -> value. The Device variants
// omit the unit level, which is detected by the top-level keys being band
// codes.
func leaves(m map[string]any) []leaf {
	paths := descend([]path{{v: m}}, 4)
	if isBandMap(m) {
		paths = descend([]path{{keys: []string{""}, v: m}}, 3)
	}

	out := make([]leaf, 0, len(paths))
	for _, p := range paths {
		mac := rpc.MAC(p.keys[3])
		if mac == nil {
			continue
		}
		out = append(out, leaf{
			unit:    rpc.MAC(p.keys[0]),
			mac:     mac,
			band:    rpc.KeyName(rpc.BandNames, p.keys[1]),
			network: rpc.KeyName(rpc.NetworkNames, p.keys[2]),
			v:       p.v,
		})
	}
	return out
}

// isBandMap reports whether the keys of m are band codes rather than unit
// MACs.
func isBandMap(m map[string]any) bool {
	for k := range m {
		if _, err := strconv.Atoi(k); err == nil {
			return true
		}
	}
	return false
}

// parseAssociations decodes a GetWifiClientInfo result, sorted by MAC.
func parseAssociations(m map[string]any) []Association {
	out := []Association{}
	for _, l := range leaves(m) {
		o, ok := l.v.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, parseAssociation(l, o))
	}

	slices.SortFunc(out, func(a, b Association) int { return cmp.Compare(a.MAC.String(), b.MAC.String()) })
	return out
}

func parseAssociation(l leaf, o map[string]any) Association {
	w := Association{
		MAC:            l.mac,
		Unit:           l.unit,
		Band:           l.band,
		Network:        l.network,
		Signal:         int(rpc.Int(o, wifiSignal)),
		Inactive:       int(rpc.Int(o, wifiInactive)),
		RxBytes:        rpc.Int(o, wifiRxBytes),
		TxBytes:        rpc.Int(o, wifiTxBytes),
		RxBitrate:      int(rpc.Int(o, wifiRxBitrate)),
		TxBitrate:      int(rpc.Int(o, wifiTxBitrate)),
		RxMCS:          int(rpc.Int(o, wifiRxMCS)),
		TxMCS:          int(rpc.Int(o, wifiTxMCS)),
		RxMHz:          int(rpc.Int(o, wifiRxMHz)),
		TxMHz:          int(rpc.Int(o, wifiTxMHz)),
		SpatialStreams: int(rpc.Int(o, wifiStreams)),
		MaxBandwidth:   int(rpc.Int(o, wifiBandwidth)),
		Happiness:      int(rpc.Int(o, wifiHappiness)),
		LeaseValidity:  int(rpc.Int(o, wifiLease)),
		HostName:       text(o[wifiHostName]),
		Description:    text(o[wifiDescription]),
	}
	if mode, ok := wire.ToInt64(o[wifiMode]); ok {
		w.Mode = rpc.CodeName(modeNames, mode)
	}
	if mode, ok := wire.ToInt64(o[wifiRadioMode]); ok {
		w.RadioMode = rpc.CodeName(modeNames, mode)
	}
	w.IP = rpc.Addr(o[wifiIP])
	return w
}

// parseSignals decodes a GetWifiClientSignalQuality result, sorted by MAC.
func parseSignals(m map[string]any) []Signal {
	out := []Signal{}
	for _, l := range leaves(m) {
		q, ok := wire.ToInt64(l.v)
		if !ok {
			continue
		}
		out = append(out, Signal{MAC: l.mac, Unit: l.unit, Band: l.band, Network: l.network, Quality: int(q)})
	}

	slices.SortFunc(out, func(a, b Signal) int { return cmp.Compare(a.MAC.String(), b.MAC.String()) })
	return out
}

// text reads a msgpack string that may arrive as str or bin.
func text(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	}
	return ""
}
