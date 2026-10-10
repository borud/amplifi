// Package cli implements the amplifi command-line interface.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
)

// Options are the top-level command-line options and subcommands.
type Options struct {
	LogLevel  string           `kong:"help='log level: debug, info, warn or error',default='info',enum='debug,info,warn,error'"`
	LogFormat string           `kong:"help='log format: text or json',default='text',enum='text,json'"`
	Output    string           `kong:"short='o',help='output format: text or json',default='text',enum='text,json'"`
	Version   kong.VersionFlag `kong:"help='print the version and exit'"`

	Discover   discoverCmd      `kong:"cmd,help='find AmpliFi units on the local network'"`
	Login      loginCmd         `kong:"cmd,help='log in and save the login'"`
	Logout     logoutCmd        `kong:"cmd,help='delete a saved login'"`
	Status     statusCmd        `kong:"cmd,help='show a health summary of a unit and its mesh'"`
	Watch      watchCmd         `kong:"cmd,help='print events from a unit as they happen'"`
	Device     deviceCmd        `kong:"cmd,help='unit information, properties and display settings'"`
	Mesh       meshCmd          `kong:"cmd,help='mesh points and units'"`
	Clients    clientsCmd       `kong:"cmd,help='clients on the network'"`
	Wifi       wifiCmd          `kong:"cmd,help='Wi-Fi networks, radios and channels'"`
	Net        netCmd           `kong:"cmd,help='WAN, LAN, interfaces, UPnP and QoS'"`
	Dhcp       dhcpCmd          `kong:"cmd,help='DHCP pools and static leases'"`
	Portfwd    portfwdCmd       `kong:"cmd,help='port forwarding rules'"`
	Access     accessCmd        `kong:"cmd,help='scheduled access control (parental controls)'"`
	Firmware   fwCmd            `kong:"cmd,help='firmware upgrades'"`
	Speedtest  speedtestCmd     `kong:"cmd,help='internet speed tests'"`
	Iperf      systemIperfCmd   `kong:"cmd,help='start an iperf server on the unit for 30 seconds'"`
	Site       siteCmd          `kong:"cmd,help='site configuration: time zone, DFS and other mesh-wide settings'"`
	Admin      adminCmd         `kong:"cmd,help='admin password'"`
	Support    systemSupportCmd `kong:"cmd,help='support bundles'"`
	Stats      systemStatsCmd   `kong:"cmd,help='statistics'"`
	Homekit    systemHomeKitCmd `kong:"cmd,help='HomeKit service'"`
	Teleport   teleportCmd      `kong:"cmd,help='Teleport remote access'"`
	Reboot     rebootCmd        `kong:"cmd,help='reboot a unit'"`
	Raw        rawCmd           `kong:"cmd,help='direct method, property and ubus calls'"`
	VersionCmd versionCmd       `kong:"cmd,name='version',help='show build version, commit and time'"`
}

// JSON reports whether JSON output was selected.
func (o *Options) JSON() bool {
	return o.Output == "json"
}

// Logger builds the logger selected by the global flags.
func (o *Options) Logger() *slog.Logger {
	level := slog.LevelInfo
	switch o.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		panic(fmt.Sprintf("unexpected log level %q", o.LogLevel))
	}

	opts := &slog.HandlerOptions{Level: level}
	if o.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}
