package cli

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/kong"
	"github.com/borud/amplifi"
)

// printJSON writes v to stdout as indented JSON.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// readable rewrites decoded msgpack for display: binary values and binary
// map keys become MAC strings when they are 6 bytes long and hex otherwise.
func readable(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[readableKey(k)] = readable(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = readable(val)
		}
		return out
	case []byte:
		return binaryString(t)
	default:
		return v
	}
}

func readableKey(k string) string {
	if utf8.ValidString(k) && !slices.ContainsFunc([]rune(k), func(r rune) bool { return !unicode.IsPrint(r) }) {
		return k
	}
	return binaryString([]byte(k))
}

func binaryString(b []byte) string {
	if len(b) == 6 {
		return net.HardwareAddr(b).String()
	}
	return hex.EncodeToString(b)
}

// newTable returns an aligned table writer in the CLI's standard layout.
func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}

// printPairs prints label/value rows as an aligned table.
func printPairs(w io.Writer, rows [][2]string) error {
	tw := newTable(w)
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", r[0], r[1])
	}
	return tw.Flush()
}

// printValues prints a map of loosely typed values to stdout, one key per
// row in key order.
func printValues(m map[string]any) error {
	rows := make([][2]string, 0, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		rows = append(rows, [2]string{k, inlineValue(m[k])})
	}
	return printPairs(os.Stdout, rows)
}

// nonEmptyPairs drops the rows whose value is empty.
func nonEmptyPairs(rows [][2]string) [][2]string {
	return slices.DeleteFunc(rows, func(r [2]string) bool { return r[1] == "" })
}

// nonEmpty trims the strings in list and drops the empty ones, so an empty
// list flag such as --dns= clears the list.
func nonEmpty(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// joinMACs formats MAC addresses as a comma separated list, or "-" for
// none.
func joinMACs(macs []amplifi.MAC) string {
	s := make([]string, 0, len(macs))
	for _, m := range macs {
		s = append(s, m.String())
	}
	return orDash(strings.Join(s, ", "))
}

// onOff formats a boolean setting.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// parseOnOff parses an on/off flag value.
func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	default:
		return false, fmt.Errorf("want on or off, got %q", s)
	}
}

// onOffFlag is a kong flag value that accepts on/off (and true/false,
// yes/no, 1/0). Use a pointer field so an absent flag means unchanged.
type onOffFlag bool

// Decode implements kong.MapperValue.
func (f *onOffFlag) Decode(ctx *kong.DecodeContext) error {
	var s string
	if err := ctx.Scan.PopValueInto("value", &s); err != nil {
		return err
	}
	b, err := parseOnOff(s)
	if err != nil {
		return err
	}
	*f = onOffFlag(b)
	return nil
}

// channelText formats a Wi-Fi channel. The router stores automatic as -1; 0
// is treated the same.
func channelText(ch int64) string {
	if ch <= 0 {
		return "auto"
	}
	return strconv.FormatInt(ch, 10)
}

// parseChannel parses a channel number or "auto" (-1).
func parseChannel(s string) (int, error) {
	if strings.EqualFold(s, "auto") {
		return -1, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("want a channel number or auto, got %q", s)
	}
	return n, nil
}

// clockText formats minutes after midnight as HH:MM.
func clockText(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// parseClock parses HH:MM into minutes after midnight.
func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("time %q: want HH:MM", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// unixTime formats Unix seconds, or "-" for none.
func unixTime(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).Format(time.DateTime)
}

// mbit formats a kbit/s rate as Mbit/s.
func mbit(kbit int64) string {
	return strconv.FormatFloat(float64(kbit)/1000, 'f', 1, 64) + " Mbit/s"
}

// orDash returns s, or "-" when it is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// intOrDash formats an optional integer, or "-" when it is unset.
func intOrDash(p *int) string {
	if p == nil {
		return "-"
	}
	return strconv.Itoa(*p)
}

// findOne picks the item a command argument refers to: an exact key match,
// else exactly one item with a matching name (ignoring case). kind names the
// items in errors. names may be nil when items are only known by key.
func findOne[T any](items []T, ref, kind string, key func(T) string, names func(T) []string) (T, error) {
	var zero T
	for _, it := range items {
		if strings.EqualFold(key(it), ref) {
			return it, nil
		}
	}
	if names == nil {
		return zero, fmt.Errorf("no %s %q", kind, ref)
	}

	var found []T
	for _, it := range items {
		if slices.ContainsFunc(names(it), func(n string) bool { return strings.EqualFold(n, ref) }) {
			found = append(found, it)
		}
	}
	switch len(found) {
	case 0:
		return zero, fmt.Errorf("no %s %q", kind, ref)
	case 1:
		return found[0], nil
	default:
		return zero, fmt.Errorf("%q matches %d %ss; use the ID or MAC address", ref, len(found), kind)
	}
}

// inlineValue renders a value on one line: strings as is, anything else as
// compact JSON.
func inlineValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(readable(v))
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// parseValue parses a command-line value as JSON (integers stay integers),
// falling back to the plain string.
func parseValue(s string) any {
	v, err := parseJSONArg(s)
	if err != nil {
		return s
	}
	return v
}

// parseJSONArg parses a JSON argument, turning integral numbers into int64
// so they are sent as msgpack integers rather than floats.
func parseJSONArg(s string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("parse JSON argument: %w", err)
	}
	return jsonNumbers(v), nil
}

func jsonNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = jsonNumbers(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = jsonNumbers(val)
		}
		return t
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		f, _ := t.Float64()
		return f
	default:
		return v
	}
}

// readableMap is readable for a map, returning an empty map for anything
// else.
func readableMap(m map[string]any) map[string]any {
	out, ok := readable(m).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return out
}

// partError records a part of a multi-part report that the router refused,
// so one unsupported method does not hide the rest. Any other error is
// returned, as it means the session is gone.
func partError(errs map[string]string, part string, err error) error {
	if err == nil {
		return nil
	}
	if !amplifi.IsRouterError(err) {
		return fmt.Errorf("%s: %w", part, err)
	}
	errs[part] = err.Error()
	return nil
}

// changeSet collects the changes a write command is about to make, to show
// them before asking for confirmation. A new item shows as a change from
// "-", a deleted one as a change to "-".
//
// The typed methods take the flag value (nil when the flag was not given)
// and the current value, record the change and return the value to send:
// nil when the flag was not given or was invalid.
type changeSet struct {
	rows [][3]string
	errs []error
}

// add records a change of field from old to new. Unchanged values are
// skipped.
func (cs *changeSet) add(field, from, to string) {
	if from == to {
		return
	}
	cs.rows = append(cs.rows, [3]string{field, from, to})
}

// create records a field of a new item. Empty values are skipped.
func (cs *changeSet) create(field, value string) {
	cs.add(field, "-", orDash(value))
}

// remove records the deletion of an item, described by desc.
func (cs *changeSet) remove(field, desc string) {
	cs.rows = append(cs.rows, [3]string{field, desc, "-"})
}

// fail records an invalid flag value.
func (cs *changeSet) fail(field string, err error) {
	cs.errs = append(cs.errs, fmt.Errorf("%s: %w", field, err))
}

// onOff records an on/off setting.
func (cs *changeSet) onOff(field string, v *onOffFlag, old bool) *bool {
	if v == nil {
		return nil
	}
	b := bool(*v)
	cs.add(field, onOff(old), onOff(b))
	return &b
}

// text records a string setting. Empty values show as "-".
func (cs *changeSet) text(field string, v *string, old string) *string {
	if v != nil {
		cs.add(field, orDash(old), orDash(*v))
	}
	return v
}

// secret records a password change without showing either value.
func (cs *changeSet) secret(field string, v *string) *string {
	if v != nil {
		cs.add(field, "(hidden)", secretText(*v))
	}
	return v
}

// secretText stands in for a new password in a change summary.
func secretText(s string) string {
	if s == "" {
		return "-"
	}
	return "(new)"
}

// number records an integer setting.
func (cs *changeSet) number(field string, v *int, old int64) *int {
	if v != nil {
		cs.add(field, strconv.FormatInt(old, 10), strconv.Itoa(*v))
	}
	return v
}

// channel records a Wi-Fi channel given as a number or "auto".
func (cs *changeSet) channel(field string, v *string, old int64) *int {
	if v == nil {
		return nil
	}
	ch, err := parseChannel(*v)
	if err != nil {
		cs.fail(field, err)
		return nil
	}
	cs.add(field, channelText(old), channelText(int64(ch)))
	return &ch
}

// addParsed records an enumerated setting parsed by parse.
func addParsed[T fmt.Stringer](cs *changeSet, field string, v *string, old T, parse func(string) (T, error)) *T {
	if v == nil {
		return nil
	}
	n, err := parse(*v)
	if err != nil {
		cs.fail(field, err)
		return nil
	}
	cs.add(field, old.String(), n.String())
	return &n
}

// approve reports invalid values, refuses an empty change set, prints the
// changes and asks prompt unless --yes was given.
func (cs *changeSet) approve(y yesFlag, prompt string) (bool, error) {
	if err := errors.Join(cs.errs...); err != nil {
		return false, err
	}
	if len(cs.rows) == 0 {
		return false, errors.New("nothing to change; see --help for the settings")
	}

	tw := newTable(os.Stdout)
	for _, r := range cs.rows {
		fmt.Fprintf(tw, "%s\t%s\t->\t%s\n", r[0], r[1], r[2])
	}
	if err := tw.Flush(); err != nil {
		return false, err
	}
	return y.approve(prompt), nil
}
