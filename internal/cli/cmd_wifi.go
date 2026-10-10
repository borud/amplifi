package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/wifi"
)

type wifiCmd struct {
	Show       wifiShowCmd       `kong:"cmd,help='show the Wi-Fi configuration'"`
	Set        wifiSetCmd        `kong:"cmd,help='change the Wi-Fi configuration'"`
	Guest      wifiGuestCmd      `kong:"cmd,help='guest network'"`
	IoT        wifiIoTCmd        `kong:"cmd,name='iot',help='IoT network'"`
	Radios     wifiRadiosCmd     `kong:"cmd,help='radios'"`
	Interfaces wifiInterfacesCmd `kong:"cmd,help='Wi-Fi interfaces'"`
	Channels   wifiChannelsCmd   `kong:"cmd,help='list the usable channels'"`
	Countries  wifiCountriesCmd  `kong:"cmd,help='list the supported country codes'"`
	Radar      wifiRadarCmd      `kong:"cmd,help='list channels where DFS radar was detected'"`
	Scan       wifiScanCmd       `kong:"cmd,help='Wi-Fi signal quality scan'"`
	WPS        wifiWPSCmd        `kong:"cmd,name='wps',help='WPS push-button pairing'"`
}

type wifiGuestCmd struct {
	Show    wifiGuestShowCmd    `kong:"cmd,help='show the guest network and its limits'"`
	Enable  wifiGuestEnableCmd  `kong:"cmd,help='turn the guest network on'"`
	Disable wifiGuestDisableCmd `kong:"cmd,help='turn the guest network off'"`
	Set     wifiGuestSetCmd     `kong:"cmd,help='change the guest network'"`
}

type wifiIoTCmd struct {
	Show    wifiIoTShowCmd    `kong:"cmd,help='show the IoT network'"`
	Enable  wifiIoTEnableCmd  `kong:"cmd,help='turn the IoT network on'"`
	Disable wifiIoTDisableCmd `kong:"cmd,help='turn the IoT network off'"`
	Set     wifiIoTSetCmd     `kong:"cmd,help='change the IoT network'"`
}

type wifiRadiosCmd struct {
	Show wifiRadiosShowCmd `kong:"cmd,help='list the radios'"`
	Set  wifiRadiosSetCmd  `kong:"cmd,help='change a radio'"`
}

type wifiInterfacesCmd struct {
	Show wifiInterfacesShowCmd `kong:"cmd,help='list the Wi-Fi interfaces'"`
	Set  wifiInterfacesSetCmd  `kong:"cmd,help='change a Wi-Fi interface'"`
}

type wifiScanCmd struct {
	Start wifiScanStartCmd `kong:"cmd,help='start the scan'"`
	Stop  wifiScanStopCmd  `kong:"cmd,help='stop the scan'"`
	Show  wifiScanShowCmd  `kong:"cmd,help='show the scan results'"`
}

type wifiWPSCmd struct {
	Push   wifiWPSPushCmd   `kong:"cmd,help='start push-button pairing'"`
	Cancel wifiWPSCancelCmd `kong:"cmd,help='stop push-button pairing'"`
	Status wifiWPSStatusCmd `kong:"cmd,help='show the push-button pairing state'"`
}

// wifiNetworkFlags are the flags shared by every network's set command.
type wifiNetworkFlags struct {
	SSID       *string    `kong:"name='ssid',help='network name'"`
	Encryption *string    `kong:"help='encryption: none, wep, wpa, wpa2, wpa2-wpa3 or wpa3'"`
	Hidden     *onOffFlag `kong:"help='hide the network name (on|off)',placeholder='on|off'"`
}

// plan records the shared network flags.
func (f wifiNetworkFlags) plan(cs *changeSet, old wifi.Network) wifi.NetworkUpdate {
	return wifi.NetworkUpdate{
		SSID:       cs.text("ssid", f.SSID, old.SSID),
		Encryption: addParsed(cs, "encryption", f.Encryption, old.Encryption, wifi.ParseEncryption),
		Hidden:     cs.onOff("hidden", f.Hidden, old.Hidden),
	}
}

type wifiShowCmd struct {
	unitFlags     `kong:"embed"`
	ShowPasswords bool `kong:"help='include the passwords'"`
}

// Run prints the Wi-Fi configuration.
func (c *wifiShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		w, err := wifi.Config(ctx, cl)
		if err != nil {
			return err
		}
		if !c.ShowPasswords {
			*w = w.Redacted()
		}

		if opt.JSON() {
			w.Fields = readableMap(w.Fields)
			return printJSON(w)
		}
		return wifiPrintConfig(os.Stdout, w, c.ShowPasswords)
	})
}

func wifiPrintConfig(out io.Writer, w *wifi.Configuration, passwords bool) error {
	if err := wifiPrintNetworks(out, passwords, []string{"main", "guest", "iot"}, []wifi.Network{w.Main, w.Guest, w.IoT}); err != nil {
		return err
	}
	fmt.Fprintln(out)

	single := onOff(w.SingleRadio)
	if w.SingleRadio && w.ActiveBand != "" {
		single += " (active band " + w.ActiveBand + ")"
	}
	return printPairs(out, [][2]string{
		{"band steering", onOff(w.BandSteering)},
		{"router steering", onOff(w.RouterSteering)},
		{"single radio", single},
		{"country", wifiWithRuntime(w.Country, w.RuntimeCountry)},
		{"channel 2.4GHz", wifiWithRuntime(channelText(w.Channel24), channelText(w.RuntimeChannel24))},
		{"channel 5GHz", wifiWithRuntime(channelText(w.Channel5), channelText(w.RuntimeChannel5))},
		{"802.11r fast roaming", onOff(w.FastRoaming)},
		{"802.11k radio measurement", onOff(w.RadioMeasurement)},
		{"802.11v bss transition", onOff(w.BSSTransition)},
		{"802.11w protected frames", w.PMF.String()},
		{"legacy 802.11b", onOff(w.Legacy11b)},
		{"multiple hops", onOff(w.MultipleHops)},
		{"ethernet backbone", onOff(w.EthernetBackbone)},
	})
}

func wifiPrintNetworks(out io.Writer, passwords bool, labels []string, nets []wifi.Network) error {
	tw := newTable(out)
	header := "NETWORK\tENABLED\tHIDDEN\tSSID\tENCRYPTION"
	if passwords {
		header += "\tPASSWORD"
	}
	fmt.Fprintln(tw, header)
	for i, n := range nets {
		row := fmt.Sprintf("%s\t%s\t%s\t%s\t%s", labels[i], onOff(n.Enabled), onOff(n.Hidden), n.SSID, n.Encryption)
		if passwords {
			row += "\t" + n.Password
		}
		fmt.Fprintln(tw, row)
	}
	return tw.Flush()
}

type wifiSetCmd struct {
	unitFlags        `kong:"embed"`
	yesFlag          `kong:"embed"`
	wifiNetworkFlags `kong:"embed"`
	Password         *string    `kong:"name='passphrase',help='network passphrase'"`
	Enabled          *onOffFlag `kong:"help='enable the main network (on|off)',placeholder='on|off'"`
	BandSteering     *onOffFlag `kong:"help='steer clients between bands (on|off)',placeholder='on|off'"`
	RouterSteering   *onOffFlag `kong:"help='steer clients between mesh points (on|off)',placeholder='on|off'"`
	SingleRadio      *onOffFlag `kong:"help='use a single band (on|off)',placeholder='on|off'"`
	ActiveBand       *string    `kong:"help='band used in single radio mode: 2.4 or 5'"`
	Channel24        *string    `kong:"name='channel-2g',help='2.4GHz channel or auto'"`
	Channel5         *string    `kong:"name='channel-5g',help='5GHz channel or auto'"`
	Country          *string    `kong:"help='two-letter country code'"`
	FastRoaming      *onOffFlag `kong:"name='80211r',help='802.11r fast roaming (on|off)',placeholder='on|off'"`
	RadioMeasurement *onOffFlag `kong:"name='80211k',help='802.11k radio measurement (on|off)',placeholder='on|off'"`
	BSSTransition    *onOffFlag `kong:"name='80211v',help='802.11v BSS transition (on|off)',placeholder='on|off'"`
	PMF              *string    `kong:"name='80211w',help='802.11w protected management frames: off, optional or required'"`
	Legacy11b        *onOffFlag `kong:"name='legacy-11b',help='allow 802.11b clients (on|off)',placeholder='on|off'"`
	MultipleHops     *onOffFlag `kong:"help='allow mesh points to chain (on|off)',placeholder='on|off'"`
}

// Run changes the given Wi-Fi settings after confirmation.
func (c *wifiSetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "apply these Wi-Fi changes to "+c.Host+"?", wifi.Config, c.plan)
}

func (c *wifiSetCmd) plan(cs *changeSet, w *wifi.Configuration) writeFunc {
	u := wifi.ConfigUpdate{
		Main:             c.wifiNetworkFlags.plan(cs, w.Main),
		BandSteering:     cs.onOff("band steering", c.BandSteering, w.BandSteering),
		RouterSteering:   cs.onOff("router steering", c.RouterSteering, w.RouterSteering),
		SingleRadio:      cs.onOff("single radio", c.SingleRadio, w.SingleRadio),
		ActiveBand:       cs.text("active band", c.ActiveBand, w.ActiveBand),
		Country:          cs.text("country", c.Country, w.Country),
		Channel24:        cs.channel("channel 2.4GHz", c.Channel24, w.Channel24),
		Channel5:         cs.channel("channel 5GHz", c.Channel5, w.Channel5),
		FastRoaming:      cs.onOff("802.11r", c.FastRoaming, w.FastRoaming),
		RadioMeasurement: cs.onOff("802.11k", c.RadioMeasurement, w.RadioMeasurement),
		BSSTransition:    cs.onOff("802.11v", c.BSSTransition, w.BSSTransition),
		PMF:              addParsed(cs, "802.11w", c.PMF, w.PMF, wifi.ParsePMF),
		Legacy11b:        cs.onOff("legacy 802.11b", c.Legacy11b, w.Legacy11b),
		MultipleHops:     cs.onOff("multiple hops", c.MultipleHops, w.MultipleHops),
	}
	u.Main.Password = cs.secret("passphrase", c.Password)
	u.Main.Enabled = cs.onOff("enabled", c.Enabled, w.Main.Enabled)
	return func(ctx context.Context, cl *amplifi.Client) error { return wifi.UpdateConfig(ctx, cl, u) }
}

type wifiGuestShowCmd struct {
	unitFlags     `kong:"embed"`
	ShowPasswords bool `kong:"help='include the password'"`
}

// Run prints the guest network.
func (c *wifiGuestShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		g, err := wifi.Guest(ctx, cl)
		if err != nil {
			return err
		}
		if !c.ShowPasswords {
			*g = g.Redacted()
		}

		if opt.JSON() {
			return printJSON(g)
		}
		if err := wifiPrintNetworks(os.Stdout, c.ShowPasswords, []string{"guest"}, []wifi.Network{g.Network}); err != nil {
			return err
		}
		fmt.Println()
		return printPairs(os.Stdout, [][2]string{
			{"client limit", strconv.FormatInt(g.ClientLimit, 10)},
			{"duration limit", strconv.FormatInt(g.DurationLimit, 10)},
			{"disable time", g.DisableTime},
		})
	})
}

type wifiGuestEnableCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run turns the guest network on.
func (c *wifiGuestEnableCmd) Run(opt *Options) error {
	return wifiSetGuestEnabled(opt, &c.unitFlags, c.yesFlag, true)
}

type wifiGuestDisableCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run turns the guest network off.
func (c *wifiGuestDisableCmd) Run(opt *Options) error {
	return wifiSetGuestEnabled(opt, &c.unitFlags, c.yesFlag, false)
}

func wifiSetGuestEnabled(opt *Options, u *unitFlags, y yesFlag, on bool) error {
	flag := onOffFlag(on)
	return apply(opt, u, y, "change the guest network on "+u.Host+"?", wifi.Guest,
		func(cs *changeSet, g *wifi.GuestNetwork) writeFunc {
			upd := wifi.GuestUpdate{NetworkUpdate: wifi.NetworkUpdate{
				Enabled: cs.onOff("enabled", &flag, g.Enabled),
			}}
			return func(ctx context.Context, cl *amplifi.Client) error { return wifi.UpdateGuest(ctx, cl, upd) }
		})
}

type wifiGuestSetCmd struct {
	unitFlags        `kong:"embed"`
	yesFlag          `kong:"embed"`
	wifiNetworkFlags `kong:"embed"`
	Password         *string `kong:"name='passphrase',help='network passphrase'"`
	ClientLimit      *int    `kong:"help='maximum number of guest clients'"`
	DurationLimit    *int    `kong:"help='guest access duration limit (raw router value)'"`
	DisableTime      *string `kong:"help='guest network disable time (raw router value)'"`
}

// Run changes the given guest network settings after confirmation.
func (c *wifiGuestSetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "change the guest network on "+c.Host+"?", wifi.Guest, c.plan)
}

func (c *wifiGuestSetCmd) plan(cs *changeSet, g *wifi.GuestNetwork) writeFunc {
	u := wifi.GuestUpdate{
		NetworkUpdate: c.wifiNetworkFlags.plan(cs, g.Network),
		ClientLimit:   cs.number("client limit", c.ClientLimit, g.ClientLimit),
		DurationLimit: cs.number("duration limit", c.DurationLimit, g.DurationLimit),
		DisableTime:   cs.text("disable time", c.DisableTime, g.DisableTime),
	}
	u.Password = cs.secret("passphrase", c.Password)
	return func(ctx context.Context, cl *amplifi.Client) error { return wifi.UpdateGuest(ctx, cl, u) }
}

type wifiIoTShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the IoT network.
func (c *wifiIoTShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		n, err := wifi.IoT(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(n)
		}
		return wifiPrintNetworks(os.Stdout, false, []string{"iot"}, []wifi.Network{*n})
	})
}

type wifiIoTEnableCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run turns the IoT network on.
func (c *wifiIoTEnableCmd) Run(opt *Options) error {
	return wifiSetIoTEnabled(opt, &c.unitFlags, c.yesFlag, true)
}

type wifiIoTDisableCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run turns the IoT network off.
func (c *wifiIoTDisableCmd) Run(opt *Options) error {
	return wifiSetIoTEnabled(opt, &c.unitFlags, c.yesFlag, false)
}

func wifiSetIoTEnabled(opt *Options, u *unitFlags, y yesFlag, on bool) error {
	flag := onOffFlag(on)
	return apply(opt, u, y, "change the IoT network on "+u.Host+"?", wifi.IoT,
		func(cs *changeSet, n *wifi.Network) writeFunc {
			upd := wifi.NetworkUpdate{Enabled: cs.onOff("enabled", &flag, n.Enabled)}
			return func(ctx context.Context, cl *amplifi.Client) error { return wifi.UpdateIoT(ctx, cl, upd) }
		})
}

type wifiIoTSetCmd struct {
	unitFlags        `kong:"embed"`
	yesFlag          `kong:"embed"`
	wifiNetworkFlags `kong:"embed"`
}

// Run changes the given IoT network settings after confirmation.
func (c *wifiIoTSetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "change the IoT network on "+c.Host+"?", wifi.IoT,
		func(cs *changeSet, n *wifi.Network) writeFunc {
			upd := c.wifiNetworkFlags.plan(cs, *n)
			return func(ctx context.Context, cl *amplifi.Client) error { return wifi.UpdateIoT(ctx, cl, upd) }
		})
}

type wifiRadiosShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the radios.
func (c *wifiRadiosShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		radios, err := wifi.Radios(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			for i := range radios {
				radios[i].Fields = readableMap(radios[i].Fields)
			}
			return printJSON(radios)
		}
		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "RADIO\tBAND\tMODE\tENABLED\tUP\tCHANNEL\tWIDTH\tMAX WIDTH\tDFS\tCOUNTRY")
		for _, r := range radios {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Name, r.Band, r.Mode, onOff(r.Enabled), onOff(r.Up),
				wifiWithRuntime(channelText(r.Channel), channelText(r.RuntimeChannel)),
				wifiWithRuntime(r.Bandwidth.String(), r.RuntimeBandwidth.String()),
				r.MaxBandwidth, onOff(r.DFSSupported), r.Country)
		}
		return tw.Flush()
	})
}

type wifiRadiosSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Radio     string     `kong:"arg,help='radio name or band, as listed by wifi radios show'"`
	Enabled   *onOffFlag `kong:"help='enable the radio (on|off)',placeholder='on|off'"`
	Channel   *string    `kong:"help='channel or auto'"`
	Bandwidth *string    `kong:"help='channel width: 20, 40, 80, 80+80, 160 or max'"`
	Country   *string    `kong:"help='two-letter country code'"`
	Legacy11b *onOffFlag `kong:"name='legacy-11b',help='allow 802.11b clients (on|off)',placeholder='on|off'"`
}

// Run changes the given radio settings after confirmation.
func (c *wifiRadiosSetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change radio %s on %s?", c.Radio, c.Host), c.read, c.plan)
}

func (c *wifiRadiosSetCmd) read(ctx context.Context, cl *amplifi.Client) (wifi.Radio, error) {
	radios, err := wifi.Radios(ctx, cl)
	if err != nil {
		return wifi.Radio{}, err
	}
	return findOne(radios, c.Radio, "radio",
		func(r wifi.Radio) string { return r.Name },
		func(r wifi.Radio) []string { return []string{r.Band} })
}

func (c *wifiRadiosSetCmd) plan(cs *changeSet, r wifi.Radio) writeFunc {
	u := wifi.RadioUpdate{
		Enabled:   cs.onOff("enabled", c.Enabled, r.Enabled),
		Channel:   cs.channel("channel", c.Channel, r.Channel),
		Bandwidth: addParsed(cs, "bandwidth", c.Bandwidth, r.Bandwidth, wifi.ParseBandwidth),
		Country:   cs.text("country", c.Country, r.Country),
		Legacy11b: cs.onOff("legacy 802.11b", c.Legacy11b, r.Legacy11b),
	}
	return func(ctx context.Context, cl *amplifi.Client) error { return wifi.UpdateRadio(ctx, cl, r.Name, u) }
}

type wifiInterfacesShowCmd struct {
	unitFlags     `kong:"embed"`
	ShowPasswords bool `kong:"help='include the passwords'"`
}

// Run lists the Wi-Fi interfaces.
func (c *wifiInterfacesShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		ifaces, err := wifi.Interfaces(ctx, cl)
		if err != nil {
			return err
		}
		for i := range ifaces {
			if !c.ShowPasswords {
				ifaces[i] = ifaces[i].Redacted()
			}
			ifaces[i].Fields = readableMap(ifaces[i].Fields)
		}

		if opt.JSON() {
			return printJSON(ifaces)
		}
		tw := newTable(os.Stdout)
		header := "INTERFACE\tRADIO\tNETWORK\tMODE\tENABLED\tSSID\tENCRYPTION\tBSSID"
		if c.ShowPasswords {
			header += "\tPASSWORD"
		}
		fmt.Fprintln(tw, header)
		for _, wi := range ifaces {
			bssid := wi.BSSID
			if len(bssid) == 0 {
				bssid = wi.MAC
			}
			row := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s",
				wi.Key, wi.Device, wi.Network, wi.OperationMode, onOff(wi.Enabled), wi.SSID, wi.Encryption, bssid)
			if c.ShowPasswords {
				row += "\t" + wi.Password
			}
			fmt.Fprintln(tw, row)
		}
		return tw.Flush()
	})
}

type wifiInterfacesSetCmd struct {
	unitFlags      `kong:"embed"`
	yesFlag        `kong:"embed"`
	Interface      string     `kong:"arg,help='interface, as listed by wifi interfaces show'"`
	Enabled        *onOffFlag `kong:"help='enable the interface (on|off)',placeholder='on|off'"`
	SSID           *string    `kong:"name='ssid',help='network name'"`
	Password       *string    `kong:"name='passphrase',help='network passphrase'"`
	Encryption     *string    `kong:"help='encryption: none, wep, wpa, wpa2, wpa2-wpa3 or wpa3'"`
	BandSteering   *onOffFlag `kong:"help='steer clients between bands (on|off)',placeholder='on|off'"`
	RouterSteering *onOffFlag `kong:"help='steer clients between mesh points (on|off)',placeholder='on|off'"`
}

// Run changes the given interface settings after confirmation.
func (c *wifiInterfacesSetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change interface %s on %s?", c.Interface, c.Host), c.read, c.plan)
}

func (c *wifiInterfacesSetCmd) read(ctx context.Context, cl *amplifi.Client) (wifi.Interface, error) {
	ifaces, err := wifi.Interfaces(ctx, cl)
	if err != nil {
		return wifi.Interface{}, err
	}
	return findOne(ifaces, c.Interface, "interface",
		func(wi wifi.Interface) string { return wi.Key },
		func(wi wifi.Interface) []string { return []string{wi.Name} })
}

func (c *wifiInterfacesSetCmd) plan(cs *changeSet, wi wifi.Interface) writeFunc {
	u := wifi.InterfaceUpdate{
		Enabled:        cs.onOff("enabled", c.Enabled, wi.Enabled),
		SSID:           cs.text("ssid", c.SSID, wi.SSID),
		Password:       cs.secret("passphrase", c.Password),
		Encryption:     addParsed(cs, "encryption", c.Encryption, wi.Encryption, wifi.ParseEncryption),
		BandSteering:   cs.onOff("band steering", c.BandSteering, wi.BandSteering),
		RouterSteering: cs.onOff("router steering", c.RouterSteering, wi.RouterSteering),
	}
	return func(ctx context.Context, cl *amplifi.Client) error { return wifi.UpdateInterface(ctx, cl, wi.Key, u) }
}

type wifiChannelsCmd struct {
	unitFlags `kong:"embed"`
	Info      bool `kong:"help='show per-channel details (frequency, power, width, DFS)'"`
}

// Run lists the usable channels, or their details with --info.
func (c *wifiChannelsCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		if !c.Info {
			channels, err := wifi.Channels(ctx, cl)
			if err != nil {
				return err
			}
			return wifiPrintInts(opt, channels)
		}

		info, err := wifi.ChannelInfo(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(info)
		}
		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "BAND\tCHANNEL\tMHZ\tWIDTH\tTX POWER\tRADAR\tDFS STATE")
		for _, band := range slices.Sorted(maps.Keys(info)) {
			for _, ch := range info[band] {
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\t%s\n",
					band, ch.Channel, ch.Freq, ch.Width, ch.Power, onOff(ch.Radar), intOrDash(ch.DFSState))
			}
		}
		return tw.Flush()
	})
}

type wifiCountriesCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the supported country codes.
func (c *wifiCountriesCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		codes, err := wifi.Countries(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(codes)
		}
		fmt.Println(strings.Join(codes, " "))
		return nil
	})
}

type wifiRadarCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the channels with detected radar.
func (c *wifiRadarCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		radar, err := wifi.Radar(ctx, cl)
		if err != nil {
			return err
		}
		radar = readableMap(radar)

		if opt.JSON() {
			return printJSON(radar)
		}
		if len(radar) == 0 {
			fmt.Println("no radar events")
			return nil
		}
		return printValues(radar)
	})
}

type wifiScanStartCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run starts the scan.
func (c *wifiScanStartCmd) Run(opt *Options) error {
	prompt := fmt.Sprintf("start the Wi-Fi signal quality scan on %s? This changes radio state", c.Host)
	return c.run(opt, c.yesFlag, prompt, wifi.StartScan)
}

type wifiScanStopCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run stops the scan.
func (c *wifiScanStopCmd) Run(opt *Options) error {
	prompt := fmt.Sprintf("stop the Wi-Fi signal quality scan on %s? This changes radio state", c.Host)
	return c.run(opt, c.yesFlag, prompt, wifi.StopScan)
}

type wifiScanShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the scan results.
func (c *wifiScanShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		entries, err := wifi.Scan(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(entries)
		}
		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "BAND\tNETWORK\tMAC\tQUALITY")
		for _, e := range entries {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", e.Band, e.Network, e.MAC, e.Quality)
		}
		return tw.Flush()
	})
}

type wifiWPSPushCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run starts WPS push-button pairing.
func (c *wifiWPSPushCmd) Run(opt *Options) error {
	prompt := fmt.Sprintf("start WPS push-button pairing on %s? Any device pressing WPS can join", c.Host)
	return c.run(opt, c.yesFlag, prompt, wifi.StartWPS)
}

type wifiWPSCancelCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run stops WPS push-button pairing.
func (c *wifiWPSCancelCmd) Run(opt *Options) error {
	return c.run(opt, c.yesFlag, fmt.Sprintf("cancel WPS push-button pairing on %s?", c.Host), wifi.CancelWPS)
}

type wifiWPSStatusCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the WPS pairing state.
func (c *wifiWPSStatusCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		st, err := wifi.WPSStatus(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(st)
		}
		return printValues(st)
	})
}

func wifiPrintInts(opt *Options, list []int64) error {
	if opt.JSON() {
		return printJSON(list)
	}
	words := make([]string, len(list))
	for i, n := range list {
		words[i] = strconv.FormatInt(n, 10)
	}
	fmt.Println(strings.Join(words, " "))
	return nil
}

// wifiWithRuntime shows a configured value with the value in use when they
// differ.
func wifiWithRuntime(configured, runtime string) string {
	if runtime == "" || runtime == configured || runtime == "auto" || runtime == "#0" {
		return configured
	}
	return configured + " (now " + runtime + ")"
}
