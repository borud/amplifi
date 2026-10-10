package cli

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/device"
	"github.com/borud/amplifi/internal/rpc"
)

type deviceCmd struct {
	Info         deviceInfoCmd         `kong:"cmd,help='show the system board'"`
	Firmware     deviceFirmwareCmd     `kong:"cmd,help='show the running firmware and any pending upgrade'"`
	Perf         devicePerfCmd         `kong:"cmd,help='show current and peak WAN throughput'"`
	MACs         deviceMACsCmd         `kong:"cmd,name='macs',help='list the unit\\'s MAC addresses by interface'"`
	Config       deviceConfigCmd       `kong:"cmd,help='show the device configuration (identity, locale, role)'"`
	Props        devicePropsCmd        `kong:"cmd,help='show every known property'"`
	Prop         devicePropCmd         `kong:"cmd,help='single properties'"`
	Display      deviceDisplayCmd      `kong:"cmd,help='touchscreen, LED and sound settings'"`
	Reboot       rebootCmd             `kong:"cmd,help='reboot the unit'"`
	FactoryReset deviceFactoryResetCmd `kong:"cmd,help='erase all settings and reboot (irreversible)'"`
}

type deviceInfoCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the system board info.
func (c *deviceInfoCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		info, err := device.Board(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			out := *info
			out.Fields = readableMap(info.Fields)
			return printJSON(out)
		}

		rows := [][2]string{{"name", info.Name}, {"revision", fmt.Sprint(info.Revision)}}
		for _, k := range slices.SortedFunc(maps.Keys(info.Fields), rpc.CompareNumeric) {
			if k == "1" || k == "2" {
				continue
			}
			rows = append(rows, [2]string{"option " + k, fmt.Sprint(readable(info.Fields[k]))})
		}
		return printPairs(os.Stdout, rows)
	})
}

type deviceFirmwareCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the firmware versions.
func (c *deviceFirmwareCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		fw, err := device.Firmware(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(fw)
		}

		rows := [][2]string{{"type", fw.Type}, {"version", fw.Version}, {"revision", fw.Revision}}
		if fw.UpgradeVersion != "" {
			rows = append(rows, [2]string{"upgrade version", fw.UpgradeVersion}, [2]string{"upgrade revision", fw.UpgradeRevision})
		}
		return printPairs(os.Stdout, rows)
	})
}

type devicePerfCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the throughput counters.
func (c *devicePerfCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		p, err := device.Performance(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(p)
		}
		return printPairs(os.Stdout, [][2]string{
			{"rx", mbit(p.Rx)}, {"tx", mbit(p.Tx)},
			{"rx peak", mbit(p.RxMax)}, {"tx peak", mbit(p.TxMax)},
		})
	})
}

type deviceMACsCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the unit's MAC addresses.
func (c *deviceMACsCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		macs, err := device.MACs(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(macs)
		}

		rows := make([][2]string, 0, len(macs))
		for _, m := range macs {
			rows = append(rows, [2]string{m.Role, m.MAC.String()})
		}
		return printPairs(os.Stdout, rows)
	})
}

type deviceConfigCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the device configuration.
func (c *deviceConfigCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		cfg, err := device.Config(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			out := *cfg
			out.Fields = readableMap(cfg.Fields)
			return printJSON(out)
		}

		return printPairs(os.Stdout, [][2]string{
			{"friendly name", cfg.FriendlyName},
			{"host name", cfg.HostName},
			{"model", cfg.ModelName},
			{"platform", cfg.PlatformName},
			{"system name", cfg.SystemName},
			{"unique name", cfg.UniqueName},
			{"board", fmt.Sprintf("%s rev %d", cfg.BoardName, cfg.BoardRevision)},
			{"role", cfg.NodeRole},
			{"connectivity", cfg.ConnectivityMode},
			{"country", cfg.Country},
			{"timezone", cfg.Timezone},
			{"zone name", cfg.ZoneName},
			{"protocol version", fmt.Sprint(cfg.ProtocolVersion)},
			{"initial setup done", fmt.Sprint(cfg.InitialSetupDone)},
			{"upgrade available", fmt.Sprint(cfg.FirmwareUpgradeAvailable)},
		})
	})
}

type devicePropsCmd struct {
	unitFlags `kong:"embed"`
}

// Run reads and prints every known property.
func (c *devicePropsCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		props, err := device.Properties(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(props)
		}

		rows := make([][2]string, 0, len(props))
		for _, p := range props {
			value := inlineValue(p.Value)
			if p.Error != "" {
				value = "unavailable: " + p.Error
			}
			rows = append(rows, [2]string{p.Interface + "." + p.Name, value})
		}
		return printPairs(os.Stdout, rows)
	})
}

type devicePropCmd struct {
	Show devicePropShowCmd `kong:"cmd,help='show a property'"`
	Set  devicePropSetCmd  `kong:"cmd,help='change a property'"`
}

type devicePropShowCmd struct {
	unitFlags `kong:"embed"`
	Iface     string `kong:"arg,help='interface, e.g. Device, ClusterNode or Unsecure'"`
	Name      string `kong:"arg,help='property name, e.g. FriendlyName'"`
}

// Run reads one property.
func (c *devicePropShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		v, err := cl.Property(ctx, c.Iface, c.Name)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(readable(v))
		}
		fmt.Println(inlineValue(v))
		return nil
	})
}

type devicePropSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Iface     string `kong:"arg,help='interface, e.g. Device, ClusterNode or Unsecure'"`
	Name      string `kong:"arg,help='property name, e.g. FriendlyName'"`
	Value     string `kong:"arg,help='new value; parsed as JSON (true, 42, \"text\") and sent as a string otherwise'"`
}

// Run writes one property after showing the change.
func (c *devicePropSetCmd) Run(opt *Options) error {
	value := parseValue(c.Value)
	read := func(ctx context.Context, cl *amplifi.Client) (any, error) {
		return cl.Property(ctx, c.Iface, c.Name)
	}
	return apply(opt, &c.unitFlags, c.yesFlag, "change this property on "+c.Host+"?", read,
		func(cs *changeSet, cur any) writeFunc {
			cs.add(c.Iface+"."+c.Name, inlineValue(cur), inlineValue(value))
			return func(ctx context.Context, cl *amplifi.Client) error {
				return cl.SetProperty(ctx, c.Iface, c.Name, value)
			}
		})
}

type deviceDisplayCmd struct {
	Show deviceDisplayShowCmd `kong:"cmd,help='show the display settings'"`
	Set  deviceDisplaySetCmd  `kong:"cmd,help='change the display settings'"`
}

type deviceDisplayShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the display settings.
func (c *deviceDisplayShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		d, err := device.Display(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(d)
		}
		return printPairs(os.Stdout, deviceDisplayRows(d))
	})
}

// deviceDisplayRows lists the display settings in a fixed order.
func deviceDisplayRows(d *device.DisplaySettings) [][2]string {
	return [][2]string{
		{"sound volume", fmt.Sprint(d.SystemSoundVolume)},
		{"tap volume", fmt.Sprint(d.TapSoundVolume)},
		{"lcd brightness", fmt.Sprint(d.LCDBrightness)},
		{"led brightness", fmt.Sprint(d.LEDBrightness)},
		{"clock type", fmt.Sprintf("%dh", d.ClockType)},
		{"clock timeout", fmt.Sprint(d.ClockTimeout)},
		{"off timeout", fmt.Sprint(d.OffTimeout)},
		{"night mode", onOff(d.NightMode)},
		{"night start", clockText(d.NightStart)},
		{"night end", clockText(d.NightEnd)},
	}
}

type deviceDisplaySetCmd struct {
	unitFlags     `kong:"embed"`
	yesFlag       `kong:"embed"`
	SoundVolume   *int       `kong:"help='system sound volume'"`
	TapVolume     *int       `kong:"help='touch sound volume'"`
	LCDBrightness *int       `kong:"name='lcd-brightness',help='touchscreen brightness'"`
	LEDBrightness *int       `kong:"name='led-brightness',help='LED ring brightness'"`
	ClockType     *int       `kong:"help='clock format: 12 or 24'"`
	ClockTimeout  *int       `kong:"help='clock timeout (raw router value)'"`
	OffTimeout    *int       `kong:"help='screen off timeout (raw router value)'"`
	NightMode     *onOffFlag `kong:"help='dim the display at night (on|off)',placeholder='on|off'"`
	NightStart    string     `kong:"help='night mode start, HH:MM'"`
	NightEnd      string     `kong:"help='night mode end, HH:MM'"`
}

// Run changes the display settings after showing the change.
func (c *deviceDisplaySetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "change the display settings on "+c.Host+"?", device.Display,
		func(cs *changeSet, cur *device.DisplaySettings) writeFunc {
			u := c.update(cs)
			next := u.Apply(*cur)
			before, after := deviceDisplayRows(cur), deviceDisplayRows(&next)
			for i := range before {
				cs.add(before[i][0], before[i][1], after[i][1])
			}
			return func(ctx context.Context, cl *amplifi.Client) error { return device.UpdateDisplay(ctx, cl, u) }
		})
}

// update builds the display change from the flags, recording invalid flag
// values in cs.
func (c *deviceDisplaySetCmd) update(cs *changeSet) device.DisplayUpdate {
	u := device.DisplayUpdate{
		SystemSoundVolume: c.SoundVolume,
		TapSoundVolume:    c.TapVolume,
		LCDBrightness:     c.LCDBrightness,
		LEDBrightness:     c.LEDBrightness,
		ClockType:         c.ClockType,
		ClockTimeout:      c.ClockTimeout,
		OffTimeout:        c.OffTimeout,
		NightStart:        deviceOptionalClock(cs, "night start", c.NightStart),
		NightEnd:          deviceOptionalClock(cs, "night end", c.NightEnd),
	}
	if c.NightMode != nil {
		on := bool(*c.NightMode)
		u.NightMode = &on
	}
	return u
}

// deviceOptionalClock parses an optional HH:MM flag; empty means unchanged
// and yields nil.
func deviceOptionalClock(cs *changeSet, field, s string) *int {
	if s == "" {
		return nil
	}
	minutes, err := parseClock(s)
	if err != nil {
		cs.fail(field, err)
		return nil
	}
	return &minutes
}

type deviceFactoryResetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run factory resets the unit after the user types its name.
func (c *deviceFactoryResetCmd) Run(opt *Options) error {
	if !c.confirmName(c.Host) {
		return nil
	}
	return c.commit(opt, device.FactoryReset)
}
