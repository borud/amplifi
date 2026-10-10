// Package portforward reads and changes a unit's port forwarding rules.
package portforward

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// PortForwardingRuleConfigOption codes.
const (
	ruleID      = "1"
	ruleName    = "2"
	ruleEnabled = "3"
	ruleLANIP   = "4"
)

// PortMappingConfigOption codes.
const (
	mapID       = "1"
	mapProtocol = "2"
	mapWANPort  = "3"
	mapLANPort  = "4"
	mapWANRange = "5"
	mapLANRange = "6"
)

// Protocol is the IP protocol of a mapping, by its IP protocol number.
type Protocol int

// Protocols of a mapping.
const (
	TCP Protocol = 6
	UDP Protocol = 17
)

var protocolNames = map[string]string{"6": "tcp", "17": "udp"}

// String returns "tcp" or "udp", or "#N" for another protocol number.
func (p Protocol) String() string { return rpc.CodeName(protocolNames, int64(p)) }

// MarshalText renders the protocol by name.
func (p Protocol) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

// ParseProtocol parses "tcp" or "udp".
func ParseProtocol(s string) (Protocol, error) {
	n, err := rpc.Code("protocol", protocolNames, strings.TrimSpace(s))
	return Protocol(n), err
}

// Rule is one port forwarding rule (PortForwardingRule). A rule forwarding
// both TCP and UDP has one mapping per protocol.
type Rule struct {
	ID       int        `json:"id"`
	Name     string     `json:"name"`
	Enabled  bool       `json:"enabled"`
	LANIP    netip.Addr `json:"lan_ip"`
	Mappings []Mapping  `json:"mappings"`
}

// Mapping is one port range of a forwarding rule. WANCount and LANCount are
// the number of ports in each range (the router's *PortRangeLength); 0 and 1
// both mean a single port. LANPort 0 means the same as WANPort.
type Mapping struct {
	ID       int      `json:"id"`
	Protocol Protocol `json:"protocol"`
	WANPort  int      `json:"wan_port"`
	LANPort  int      `json:"lan_port"`
	WANCount int      `json:"wan_count"`
	LANCount int      `json:"lan_count"`
}

// RuleUpdate lists the rule fields to change; nil fields are left as they
// are. A non-nil Mappings replaces all the rule's mappings.
type RuleUpdate struct {
	Name     *string
	Enabled  *bool
	LANIP    *netip.Addr
	Mappings []Mapping
}

// Rules lists the port forwarding rules, sorted by ID.
func Rules(ctx context.Context, c *amplifi.Client) ([]Rule, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetPortForwardingConfig", nil)
	if err != nil {
		return nil, fmt.Errorf("port forwarding rules: %w", err)
	}
	return parseRules(m), nil
}

// parseRules decodes PortForwardingRules: a map of rule ID to
// [rule options, {mapping ID: mapping options}].
func parseRules(m map[string]any) []Rule {
	out := make([]Rule, 0, len(m))
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
		r := Rule{
			ID:       id,
			Name:     rpc.String(rule, ruleName),
			Enabled:  rpc.Bool(rule, ruleEnabled),
			LANIP:    rpc.Addr(rule[ruleLANIP]),
			Mappings: []Mapping{},
		}
		if mappings, ok := part(parts, 1); ok {
			r.Mappings = parseMappings(mappings)
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Rule) int { return a.ID - b.ID })
	return out
}

func parseMappings(m map[string]any) []Mapping {
	out := make([]Mapping, 0, len(m))
	for key, v := range m {
		f, ok := v.(map[string]any)
		if !ok {
			continue
		}
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		out = append(out, Mapping{
			ID:       id,
			Protocol: Protocol(rpc.Int(f, mapProtocol)),
			WANPort:  int(rpc.Int(f, mapWANPort)),
			LANPort:  max(int(rpc.Int(f, mapLANPort)), 0),
			WANCount: max(int(rpc.Int(f, mapWANRange)), 0),
			LANCount: max(int(rpc.Int(f, mapLANRange)), 0),
		})
	}
	slices.SortFunc(out, func(a, b Mapping) int { return a.ID - b.ID })
	return out
}

// pack encodes a rule as GetPortForwardingConfig returns it:
// [rule options, {mapping ID: mapping options}].
func pack(r Rule) ([]any, error) {
	ip, err := rpc.PackAddr(r.LANIP)
	if err != nil {
		return nil, err
	}
	rule := map[string]any{
		ruleID:      int64(r.ID),
		ruleName:    r.Name,
		ruleEnabled: r.Enabled,
		ruleLANIP:   ip,
	}
	mappings, err := packMappings(r.Mappings)
	if err != nil {
		return nil, err
	}
	return []any{rule, mappings}, nil
}

// packMappings encodes mappings as {mapping ID: mapping options}. Mappings
// without an ID are numbered from 1.
func packMappings(list []Mapping) (map[string]any, error) {
	mappings := map[string]any{}
	for i, pm := range list {
		f, err := packMapping(pm)
		if err != nil {
			return nil, err
		}
		id := pm.ID
		if id == 0 {
			id = i + 1
		}
		f[mapID] = int64(id)
		mappings[strconv.Itoa(id)] = f
	}
	return mappings, nil
}

func packMapping(pm Mapping) (map[string]any, error) {
	if pm.Protocol != TCP && pm.Protocol != UDP {
		return nil, fmt.Errorf("invalid protocol %s", pm.Protocol)
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
		mapProtocol: int64(pm.Protocol),
		mapWANPort:  int64(pm.WANPort),
		mapLANPort:  int64(lan),
	}
	// Single ports leave the range length out, so it does not matter
	// whether the router counts ports or the distance to the last one.
	if pm.WANCount > 1 {
		f[mapWANRange] = int64(pm.WANCount)
	}
	if pm.LANCount > 1 {
		f[mapLANRange] = int64(pm.LANCount)
	}
	return f, nil
}

// AddRule adds rule r and returns its ID. When r.ID is 0 the next free ID
// is used.
func AddRule(ctx context.Context, c *amplifi.Client, r Rule) (int, error) {
	if r.ID == 0 {
		existing, err := Rules(ctx, c)
		if err != nil {
			return 0, err
		}
		r.ID = nextID(existing)
	}

	packed, err := pack(r)
	if err != nil {
		return 0, fmt.Errorf("port forwarding rule: %w", err)
	}
	arg := map[string]any{strconv.Itoa(r.ID): packed}
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "AddPortForwardingRules", arg); err != nil {
		return 0, fmt.Errorf("add port forwarding rule %d: %w", r.ID, err)
	}
	return r.ID, nil
}

func nextID(rules []Rule) int {
	next := 1
	for _, r := range rules {
		next = max(next, r.ID+1)
	}
	return next
}

// UpdateRule changes rule id. It reads all rules, applies u to that one and
// writes them all back with SetPortForwardingConfig.
func UpdateRule(ctx context.Context, c *amplifi.Client, id int, u RuleUpdate) error {
	if u.Name == nil && u.Enabled == nil && u.LANIP == nil && u.Mappings == nil {
		return nil
	}

	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetPortForwardingConfig", nil)
	if err != nil {
		return fmt.Errorf("port forwarding rules: %w", err)
	}
	key := strconv.Itoa(id)
	rule, err := merge(m[key], u)
	if err != nil {
		return fmt.Errorf("port forwarding rule %d: %w", id, err)
	}
	m[key] = rule

	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "SetPortForwardingConfig", m); err != nil {
		return fmt.Errorf("port forwarding rule %d: %w", id, err)
	}
	return nil
}

// merge applies u to a rule as the router sent it ([rule options,
// mappings]). Only the options u sets are replaced, so everything else,
// including options this package does not know, is sent back as it was.
// The mappings are kept unless u.Mappings replaces them.
func merge(orig any, u RuleUpdate) ([]any, error) {
	parts, _ := orig.([]any)
	rule, ok := part(parts, 0)
	if !ok {
		return nil, errors.New("no such rule")
	}
	rule = maps.Clone(rule)
	rpc.Put(rule, ruleName, u.Name)
	rpc.Put(rule, ruleEnabled, u.Enabled)
	if u.LANIP != nil {
		ip, err := rpc.PackAddr(*u.LANIP)
		if err != nil {
			return nil, err
		}
		rule[ruleLANIP] = ip
	}

	mappings, ok := part(parts, 1)
	if !ok {
		mappings = map[string]any{}
	}
	if u.Mappings != nil {
		packed, err := packMappings(u.Mappings)
		if err != nil {
			return nil, err
		}
		mappings = packed
	}
	return []any{rule, mappings}, nil
}

// part returns parts[i] as a map.
func part(parts []any, i int) (map[string]any, bool) {
	if i >= len(parts) {
		return nil, false
	}
	m, ok := parts[i].(map[string]any)
	return m, ok
}

// DeleteRules deletes rules by ID.
func DeleteRules(ctx context.Context, c *amplifi.Client, ids ...int) error {
	arg := make([]any, 0, len(ids))
	for _, id := range ids {
		arg = append(arg, int64(id))
	}
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "DeletePortForwardingRules", arg); err != nil {
		return fmt.Errorf("delete port forwarding rules: %w", err)
	}
	return nil
}
