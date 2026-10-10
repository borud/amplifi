package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/clients"
	"github.com/borud/amplifi/device"
	"github.com/borud/amplifi/firmware"
	"github.com/borud/amplifi/mesh"
)

type statusCmd struct {
	unitFlags `kong:"embed"`
}

// status is everything the status command gathers. A part the unit refuses
// is nil and its error is recorded in Errors, so one unsupported method does
// not hide the rest.
type status struct {
	Board      *device.Board         `json:"board,omitempty"`
	Firmware   *device.Firmware      `json:"firmware,omitempty"`
	Updates    *firmware.Versions    `json:"updates,omitempty"`
	Internet   *device.InternetCheck `json:"internet,omitempty"`
	Throughput *device.Throughput    `json:"throughput,omitempty"`
	Clients    *clients.Counts       `json:"clients,omitempty"`
	Peers      []mesh.Peer           `json:"peers,omitempty"`
	Errors     map[string]string     `json:"errors,omitempty"`
}

// Run prints a one-screen health summary of the unit and its mesh.
func (c *statusCmd) Run(opt *Options) error {
	ctx, cancel := c.context()
	defer cancel()

	cl, err := c.dial(ctx, opt.Logger())
	if err != nil {
		return err
	}
	defer cl.Close()

	st, err := statusGather(ctx, cl)
	if err != nil {
		return err
	}

	if opt.JSON() {
		return printJSON(st)
	}
	return statusPrint(os.Stdout, c.Host, st)
}

func statusGather(ctx context.Context, cl *amplifi.Client) (*status, error) {
	st := &status{Errors: map[string]string{}}

	// Each getter fills its part of st. A router error only marks that part
	// unavailable; anything else means the session is gone.
	parts := []struct {
		name string
		get  func() error
	}{
		{"board", func() (err error) { st.Board, err = device.GetBoard(ctx, cl); return }},
		{"firmware", func() (err error) { st.Firmware, err = device.GetFirmware(ctx, cl); return }},
		{"updates", func() (err error) { st.Updates, err = firmware.GetVersions(ctx, cl); return }},
		{"internet", func() (err error) { st.Internet, err = device.CheckInternet(ctx, cl); return }},
		{"throughput", func() (err error) { st.Throughput, err = device.GetThroughput(ctx, cl); return }},
		{"clients", func() (err error) { st.Clients, err = clients.Count(ctx, cl); return }},
		{"peers", func() (err error) { st.Peers, err = mesh.Peers(ctx, cl); return }},
	}

	for _, p := range parts {
		if err := partError(st.Errors, p.name, p.get()); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func statusPrint(w io.Writer, host string, st *status) error {
	tw := newTable(w)

	if st.Board != nil {
		fmt.Fprintf(tw, "unit\t%s (%s rev %d)\n", host, st.Board.Name, st.Board.Revision)
	}
	if st.Firmware != nil {
		fmt.Fprintf(tw, "firmware\t%s%s\n", st.Firmware.Version, statusUpdateNote(st.Firmware, st.Updates))
	}
	if st.Internet != nil {
		fmt.Fprintf(tw, "internet\t%s\n", statusInternetLine(st.Internet))
	}
	if t := st.Throughput; t != nil {
		fmt.Fprintf(tw, "throughput\trx %s  tx %s  (peak rx %s  tx %s)\n",
			mbit(t.RxKbps), mbit(t.TxKbps), mbit(t.RxMaxKbps), mbit(t.TxMaxKbps))
	}
	if st.Clients != nil {
		fmt.Fprintf(tw, "wifi clients\t%d on this unit  (%s; %s)\n", st.Clients.Total, counts(st.Clients.ByBand), counts(st.Clients.ByNetwork))
	}
	for _, part := range slices.Sorted(maps.Keys(st.Errors)) {
		fmt.Fprintf(tw, "%s\tunavailable: %s\n", part, st.Errors[part])
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(st.Peers) == 0 {
		return nil
	}
	// The router only reports per-unit firmware when an update is pending.
	withFirmware := slices.ContainsFunc(st.Peers, func(p mesh.Peer) bool { return statusPeerFirmware(st.Updates, p) != "" })

	fmt.Fprintln(w)
	header := "NAME\tROLE\tIP\tMAC\tUPTIME\tBACKHAUL"
	if withFirmware {
		header += "\tFIRMWARE"
	}
	fmt.Fprintln(tw, header)
	for _, p := range st.Peers {
		backhaul := ""
		if p.Role != amplifi.RoleRouter {
			backhaul = fmt.Sprintf("%s %d dBm", p.Band, p.RSSI)
		}
		row := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", p.Name, p.Role, p.IP, p.MAC, p.Uptime.Truncate(time.Minute), backhaul)
		if withFirmware {
			row += "\t" + statusPeerFirmware(st.Updates, p)
		}
		fmt.Fprintln(tw, row)
	}
	return tw.Flush()
}

func statusUpdateNote(fw *device.Firmware, u *firmware.Versions) string {
	if fw.UpgradeVersion != "" && fw.UpgradeVersion != fw.Version {
		return "  (upgrade available: " + fw.UpgradeVersion + ")"
	}
	if u != nil && u.State != "" {
		return "  (" + fwWord(string(u.State)) + ")"
	}
	return ""
}

func statusInternetLine(in *device.InternetCheck) string {
	line := "IPv4 " + statusWord(in.HealthText, in.ReasonText)
	if in.IPv6HealthText != "" {
		line += ", IPv6 " + statusWord(in.IPv6HealthText, in.IPv6ReasonText)
	}
	if in.ISP != "" {
		line += ", ISP " + in.ISP
	}
	if in.LinkSpeed > 0 {
		line += fmt.Sprintf(", link speed %d", in.LinkSpeed)
	}
	return line
}

// statusWord shortens the router's INET_STATUS_GREEN / INET_REASON_NO_DNS
// style strings to "green" / "no dns".
func statusWord(status, reason string) string {
	s := strings.ToLower(strings.TrimPrefix(status, "INET_STATUS_"))
	r := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(reason, "INET_REASON_"), "_", " "))
	if r == "" || r == "none" {
		return s
	}
	return s + " (" + r + ")"
}

func counts(m map[string]int) string {
	parts := make([]string, 0, len(m))
	for k, n := range m {
		parts = append(parts, fmt.Sprintf("%s %d", k, n))
	}
	slices.Sort(parts)
	return strings.Join(parts, ", ")
}

func statusPeerFirmware(u *firmware.Versions, p mesh.Peer) string {
	if u == nil {
		return ""
	}
	i := slices.IndexFunc(u.Units, func(v firmware.UnitVersion) bool { return v.MAC.Equal(p.MAC) })
	if i < 0 {
		return ""
	}
	if v := u.Units[i]; v.HasUpgrade() {
		return v.Current.Version + " -> " + v.Upgrade.Version
	}
	return u.Units[i].Current.Version
}
