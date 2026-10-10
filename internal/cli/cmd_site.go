package cli

import (
	"context"
	"maps"
	"os"
	"slices"
	"strconv"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/system"
)

type siteCmd struct {
	Show siteShowCmd `kong:"cmd,help='show the site configuration'"`
	Set  siteSetCmd  `kong:"cmd,help='change the site configuration'"`
}

type siteShowCmd struct {
	unitFlags   `kong:"embed"`
	ShowSecrets bool `kong:"help='include the remote access (Teleport) credentials'"`
}

// Run prints the site configuration.
func (c *siteShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		s, err := system.Site(ctx, cl)
		if err != nil {
			return err
		}
		if !c.ShowSecrets {
			*s = s.Redacted()
		}
		if opt.JSON() {
			s.Fields = readableMap(s.Fields)
			return printJSON(s)
		}
		return sitePrint(s)
	})
}

func sitePrint(s *system.SiteConfig) error {
	rows := [][2]string{
		{"site id", strconv.FormatInt(s.SiteID, 10)},
		{"country", s.Country},
		{"time zone", s.ZoneName + "  (" + s.Timezone + ")"},
		{"clock type", strconv.FormatInt(s.ClockType, 10)},
		{"band steering", onOff(s.BandSteering)},
		{"router steering", onOff(s.RouterSteering)},
		{"multiple hops", onOff(s.MultipleHops)},
		{"channel 2.4GHz", channelText(s.Channel24)},
		{"channel 5GHz", channelText(s.Channel5)},
		{"firmware channel", s.FirmwareUpdateChannel},
		{"WPS from LCD", onOff(s.WPSFromLCD)},
		{"accept QoS overhead", onOff(s.AcceptQoSOverhead)},
		{"accept DFS interruptions", onOff(s.AcceptDFS)},
		{"guest client limit", strconv.FormatInt(s.GuestClientLimit, 10)},
		{"guest duration limit", strconv.FormatInt(s.GuestDurationLimit, 10)},
		{"guest disable time", s.GuestDisableTime},
		{"friend client limit", strconv.FormatInt(s.FriendClientLimit, 10)},
		{"friend duration limit", strconv.FormatInt(s.FriendDurationLimit, 10)},
		{"friend disable time", s.FriendDisableTime},
	}
	for _, k := range slices.Sorted(maps.Keys(s.Secrets)) {
		rows = append(rows, [2]string{k, s.Secrets[k]})
	}
	return printPairs(os.Stdout, rows)
}

type siteSetCmd struct {
	unitFlags           `kong:"embed"`
	yesFlag             `kong:"embed"`
	Timezone            *string    `kong:"help='time zone name, e.g. America/Chicago'"`
	ClockType           *int       `kong:"help='clock type shown on the display (raw router value)'"`
	WPSFromLCD          *onOffFlag `kong:"name='wps-from-lcd',help='allow starting WPS from the touchscreen (on|off)',placeholder='on|off'"`
	AcceptQoSOverhead   *onOffFlag `kong:"name='accept-qos-overhead',help='accept the QoS throughput overhead (on|off)',placeholder='on|off'"`
	AcceptDFS           *onOffFlag `kong:"name='accept-dfs-interruptions',help='use DFS channels and accept radar interruptions (on|off)',placeholder='on|off'"`
	GuestClientLimit    *int       `kong:"help='maximum number of guest Wi-Fi clients'"`
	GuestDurationLimit  *int       `kong:"help='guest Wi-Fi duration limit (raw router value)'"`
	FriendClientLimit   *int       `kong:"help='maximum number of friend Wi-Fi clients'"`
	FriendDurationLimit *int       `kong:"help='friend Wi-Fi duration limit (raw router value)'"`
}

// Run changes the site configuration after showing the change.
func (c *siteSetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "apply these changes to the whole mesh?", system.Site, c.plan)
}

func (c *siteSetCmd) plan(cs *changeSet, s *system.SiteConfig) writeFunc {
	u := system.SiteUpdate{
		ZoneName:            cs.text("time zone", c.Timezone, s.ZoneName),
		ClockType:           cs.number("clock type", c.ClockType, s.ClockType),
		WPSFromLCD:          cs.onOff("WPS from LCD", c.WPSFromLCD, s.WPSFromLCD),
		AcceptQoSOverhead:   cs.onOff("accept QoS overhead", c.AcceptQoSOverhead, s.AcceptQoSOverhead),
		AcceptDFS:           cs.onOff("accept DFS interruptions", c.AcceptDFS, s.AcceptDFS),
		GuestClientLimit:    cs.number("guest client limit", c.GuestClientLimit, s.GuestClientLimit),
		GuestDurationLimit:  cs.number("guest duration limit", c.GuestDurationLimit, s.GuestDurationLimit),
		FriendClientLimit:   cs.number("friend client limit", c.FriendClientLimit, s.FriendClientLimit),
		FriendDurationLimit: cs.number("friend duration limit", c.FriendDurationLimit, s.FriendDurationLimit),
	}
	return func(ctx context.Context, cl *amplifi.Client) error { return system.UpdateSite(ctx, cl, u) }
}
