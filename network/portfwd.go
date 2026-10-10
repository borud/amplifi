package network

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// PortForwardingRuleConfigOption codes.
const (
	portfwdRuleID  = "1"
	portfwdName    = "2"
	portfwdEnabled = "3"
	portfwdLANIP   = "4"
)

// PortMappingConfigOption codes.
const (
	portfwdMapID       = "1"
	portfwdMapProtocol = "2"
	portfwdMapWANPort  = "3"
	portfwdMapLANPort  = "4"
	portfwdMapWANRange = "5"
	portfwdMapLANRange = "6"
)

// portfwdProtocolNames maps the IP protocol numbers used by PortMapping.
var portfwdProtocolNames = map[string]string{"6": "tcp", "17": "udp"}

// PortForward is one port forwarding rule (PortForwardingRule). A rule
// forwarding both TCP and UDP has one mapping per protocol.
type PortForward struct {
	ID       int           `json:"id"`
	Name     string        `json:"name"`
	Enabled  bool          `json:"enabled"`
	LANIP    string        `json:"lan_ip"`
	Mappings []PortMapping `json:"mappings"`
}

// PortMapping is one port range of a forwarding rule. WANCount and LANCount
// are the number of ports in each range (the router's *PortRangeLength); 0
// and 1 both mean a single port. LANPort 0 means the same as WANPort.
type PortMapping struct {
	ID       int    `json:"id"`
	Protocol string `json:"protocol"`
	WANPort  int    `json:"wan_port"`
	LANPort  int    `json:"lan_port"`
	WANCount int    `json:"wan_count"`
	LANCount int    `json:"lan_count"`
}

// PortForwards lists the port forwarding rules, sorted by ID.
func PortForwards(ctx context.Context, c *amplifi.Client) ([]PortForward, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetPortForwardingConfig", nil)
	if err != nil {
		return nil, fmt.Errorf("port forwarding rules: %w", err)
	}
	return parsePortForwards(m), nil
}

// parsePortForwards decodes PortForwardingRules: a map of rule ID to
// [rule options, {mapping ID: mapping options}].
func parsePortForwards(m map[string]any) []PortForward {
	out := make([]PortForward, 0, len(m))
	for key, v := range m {
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		parts, ok := v.([]any)
		if !ok || len(parts) == 0 {
			continue
		}
		rule, _ := parts[0].(map[string]any)
		r := PortForward{
			ID:       id,
			Name:     rpc.String(rule, portfwdName),
			Enabled:  rpc.Bool(rule, portfwdEnabled),
			LANIP:    formatAddr(rule[portfwdLANIP]),
			Mappings: []PortMapping{},
		}
		if len(parts) > 1 {
			mappings, _ := parts[1].(map[string]any)
			r.Mappings = portfwdParseMappings(mappings)
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b PortForward) int { return a.ID - b.ID })
	return out
}

func portfwdParseMappings(m map[string]any) []PortMapping {
	out := make([]PortMapping, 0, len(m))
	for key, v := range m {
		f, ok := v.(map[string]any)
		if !ok {
			continue
		}
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		out = append(out, PortMapping{
			ID:       id,
			Protocol: rpc.CodeName(portfwdProtocolNames, rpc.Int(f, portfwdMapProtocol)),
			WANPort:  int(rpc.Int(f, portfwdMapWANPort)),
			LANPort:  max(int(rpc.Int(f, portfwdMapLANPort)), 0),
			WANCount: max(int(rpc.Int(f, portfwdMapWANRange)), 0),
			LANCount: max(int(rpc.Int(f, portfwdMapLANRange)), 0),
		})
	}
	slices.SortFunc(out, func(a, b PortMapping) int { return a.ID - b.ID })
	return out
}

// portfwdPack encodes a rule as GetPortForwardingConfig returns it:
// [rule options, {mapping ID: mapping options}].
func portfwdPack(r PortForward) ([]any, error) {
	ip, err := packIP(r.LANIP)
	if err != nil {
		return nil, err
	}
	rule := map[string]any{
		portfwdRuleID:  int64(r.ID),
		portfwdName:    r.Name,
		portfwdEnabled: r.Enabled,
		portfwdLANIP:   ip,
	}
	mappings, err := portfwdPackMappings(r.Mappings)
	if err != nil {
		return nil, err
	}
	return []any{rule, mappings}, nil
}

// portfwdPackMappings encodes mappings as {mapping ID: mapping options}.
// Mappings without an ID are numbered from 1.
func portfwdPackMappings(list []PortMapping) (map[string]any, error) {
	mappings := map[string]any{}
	for i, pm := range list {
		f, err := portfwdPackMapping(pm)
		if err != nil {
			return nil, err
		}
		id := pm.ID
		if id == 0 {
			id = i + 1
		}
		f[portfwdMapID] = int64(id)
		mappings[strconv.Itoa(id)] = f
	}
	return mappings, nil
}

func portfwdPackMapping(pm PortMapping) (map[string]any, error) {
	proto, err := portfwdProtocolCode(pm.Protocol)
	if err != nil {
		return nil, err
	}
	if pm.WANPort < 1 || pm.WANPort > 65535 {
		return nil, fmt.Errorf("invalid WAN port %d", pm.WANPort)
	}
	if pm.LANPort < 0 || pm.LANPort > 65535 {
		return nil, fmt.Errorf("invalid LAN port %d", pm.LANPort)
	}

	lan := pm.LANPort
	if lan == 0 {
		lan = pm.WANPort
	}
	f := map[string]any{
		portfwdMapProtocol: proto,
		portfwdMapWANPort:  int64(pm.WANPort),
		portfwdMapLANPort:  int64(lan),
	}
	// Single ports leave the range length out, so it does not matter
	// whether the router counts ports or the distance to the last one.
	if pm.WANCount > 1 {
		f[portfwdMapWANRange] = int64(pm.WANCount)
	}
	if pm.LANCount > 1 {
		f[portfwdMapLANRange] = int64(pm.LANCount)
	}
	return f, nil
}

func portfwdProtocolCode(s string) (int64, error) {
	return rpc.Code("port forwarding protocol", portfwdProtocolNames, s)
}

// AddPortForward adds a rule and returns its ID. The ID is chosen by the
// client: when r.ID is 0 the next free ID is used.
func AddPortForward(ctx context.Context, c *amplifi.Client, r PortForward) (int, error) {
	if r.ID == 0 {
		existing, err := PortForwards(ctx, c)
		if err != nil {
			return 0, err
		}
		r.ID = portfwdNextID(existing)
	}

	packed, err := portfwdPack(r)
	if err != nil {
		return 0, fmt.Errorf("port forwarding rule: %w", err)
	}
	arg := map[string]any{strconv.Itoa(r.ID): packed}
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "AddPortForwardingRules", arg); err != nil {
		return 0, fmt.Errorf("add port forwarding rule %d: %w", r.ID, err)
	}
	return r.ID, nil
}

func portfwdNextID(rules []PortForward) int {
	next := 1
	for _, r := range rules {
		next = max(next, r.ID+1)
	}
	return next
}

// PortForwardUpdate lists the rule fields to change; nil fields are left as
// they are. A non-nil Mappings replaces all the rule's mappings.
type PortForwardUpdate struct {
	Name     *string
	Enabled  *bool
	LANIP    *string
	Mappings []PortMapping
}

// UpdatePortForward changes rule id. It reads all rules, applies u to that
// one and writes them all back with SetPortForwardingConfig.
func UpdatePortForward(ctx context.Context, c *amplifi.Client, id int, u PortForwardUpdate) error {
	if u.Name == nil && u.Enabled == nil && u.LANIP == nil && u.Mappings == nil {
		return nil
	}

	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetPortForwardingConfig", nil)
	if err != nil {
		return fmt.Errorf("port forwarding rules: %w", err)
	}
	key := strconv.Itoa(id)
	rule, err := portfwdMerge(m[key], u)
	if err != nil {
		return fmt.Errorf("port forwarding rule %d: %w", id, err)
	}
	m[key] = rule

	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "SetPortForwardingConfig", m); err != nil {
		return fmt.Errorf("port forwarding rule %d: %w", id, err)
	}
	return nil
}

// portfwdMerge applies u to a rule as the router sent it
// ([rule options, mappings]). Only the options u sets are replaced, so
// everything else, including options this package does not know, is sent
// back as it was. The mappings are kept unless u.Mappings replaces them.
func portfwdMerge(orig any, u PortForwardUpdate) ([]any, error) {
	parts, _ := orig.([]any)
	rule, ok := portfwdPart(parts, 0)
	if !ok {
		return nil, errors.New("no such rule")
	}
	rule = maps.Clone(rule)
	rpc.Put(rule, portfwdName, u.Name)
	rpc.Put(rule, portfwdEnabled, u.Enabled)
	if u.LANIP != nil {
		ip, err := packIP(*u.LANIP)
		if err != nil {
			return nil, err
		}
		rule[portfwdLANIP] = ip
	}

	mappings, ok := portfwdPart(parts, 1)
	if !ok {
		mappings = map[string]any{}
	}
	if u.Mappings != nil {
		packed, err := portfwdPackMappings(u.Mappings)
		if err != nil {
			return nil, err
		}
		mappings = packed
	}
	return []any{rule, mappings}, nil
}

// portfwdPart returns parts[i] as a map.
func portfwdPart(parts []any, i int) (map[string]any, bool) {
	if i >= len(parts) {
		return nil, false
	}
	m, ok := parts[i].(map[string]any)
	return m, ok
}

// DeletePortForwards deletes rules by ID. The argument is an array of IDs.
func DeletePortForwards(ctx context.Context, c *amplifi.Client, ids ...int) error {
	arg := make([]any, 0, len(ids))
	for _, id := range ids {
		arg = append(arg, int64(id))
	}
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "DeletePortForwardingRules", arg); err != nil {
		return fmt.Errorf("delete port forwarding rules: %w", err)
	}
	return nil
}
