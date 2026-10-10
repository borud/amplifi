# amplifi

A Go library and CLI for Ubiquiti AmpliFi routers over their local control protocol, without a cloud account.

A unit is any AmpliFi box. The router is the unit connected to the internet; the other units are mesh points. A client is a device on the network.

## Install

Download a tarball for macOS or Linux (amd64 or arm64), or a `.deb`, from the [releases page](https://github.com/borud/amplifi/releases), then check it against `SHA256SUMS`:

```sh
sudo dpkg -i amplifi_*_amd64.deb   # Debian, Ubuntu, Raspberry Pi OS
```

The macOS binaries are not signed. If you downloaded one in a browser, clear the quarantine flag before running it:

```sh
xattr -d com.apple.quarantine amplifi
```

To build from source:

```sh
make install-deps  # revive and staticcheck, used by make
make               # vet, lint, test and build bin/amplifi
make install       # install amplifi into $GOBIN
```

Requires Go 1.26 or later. The library and CLI are pure Go. libsodium is only needed for `make sodium-test`, which checks the secretstream implementation against it.

## CLI

Find units on the local network:

```sh
amplifi discover
```

```text
NAME     IP          MODEL     FIRMWARE              MAC
router   192.0.2.1   AFi-R-HD  AFi-R.4.0.3.0-...     00:00:5e:00:53:01
office   192.0.2.15  AFi-P-HD  AFi-P-HD.4.0.3.0-...  00:00:5e:00:53:02
```

Log in once to save the login. The first unit you log in to becomes the default unit, which every command uses unless `-H/--host` (or `$AMPLIFI_HOST`) names another one by name, IP address or MAC address:

```sh
amplifi login 192.0.2.1             # prompts for the admin password
amplifi status                      # the default unit
amplifi status -H office            # another saved unit
amplifi login 192.0.2.15 --default  # make another unit the default
amplifi logout office               # delete a saved login
```

`--password` or `$AMPLIFI_PASSWORD` gives the password for a unit without a saved login.

Commands:

```text
status       health summary of a unit and its mesh
watch        events the unit sends, until Ctrl-C
device       board, firmware, throughput, MAC addresses, properties, display
mesh         mesh points, registered units, Ethernet ports, pairing, factory reset
clients      clients, history, signal, rename, pause and resume
wifi         networks, guest, IoT, radios, interfaces, channels, scan, WPS
net          WAN, LAN, interfaces, UPnP, QoS
dhcp         DHCP pools and static leases
portfwd      port forwarding rules
access       scheduled access control (parental controls)
firmware     update check, status and upgrade
speedtest    speed test, status and history
iperf        iperf server on the unit
site         mesh-wide settings such as time zone and DFS
admin        admin password
support      support bundle
stats        statistics reset
homekit      HomeKit service
teleport     Teleport remote access
reboot       reboot a unit
raw          any method, property or ubus call
```

Each command has `--help`. Read commands print tables, or JSON with `-o json`; a group that lists its items has a `list` subcommand, also called `ls`. Write commands print the changes and ask for confirmation; `-y/--yes` skips the question. A factory reset instead asks you to type the unit name, and only `--yes-really` skips that. Use `--log-level debug` to follow the protocol.

Saved logins are stored in `$XDG_CONFIG_HOME/amplifi/credentials.json`, by default `~/.config/amplifi/credentials.json`, with mode 0600. Each entry holds the unit's name, IP address, MAC address and SRP password hash. The password itself is not stored, but the hash grants the same access, so protect the file like a password. If the admin password changes, run `login` again.

## Library

The root package holds the session. Each functional area is its own package whose functions take the client:

```go
c, err := amplifi.Dial(ctx, "192.0.2.1", password)
if err != nil {
	return err // errors.Is(err, amplifi.ErrAuth) on a wrong password
}
defer c.Close()

board, err := device.GetBoard(ctx, c)
peers, err := mesh.Peers(ctx, c)

mac, err := amplifi.ParseMAC("00:00:5e:00:53:10")
err = clients.SetPaused(ctx, c, mac, true)

// Later, without the password:
c2, err := amplifi.DialWithHash(ctx, "192.0.2.1", c.PasswordHash())
```

The functions follow one naming scheme:

```text
GetX           fetch one value, such as device.GetBoard or wifi.GetConfig
Xs             list values, such as mesh.Peers or dhcp.StaticLeases
UpdateX        change the fields set in an XUpdate; nil fields stay as they are
SetX           replace a value, such as network.SetQoS or clients.SetPaused
AddX           add an item, returning its ID where the router has none
DeleteXs       delete items by key
StartX, StopX  start or stop background work, polled with GetX
```

Addresses are `netip.Addr` or `netip.Prefix`, MAC addresses `amplifi.MAC`, timestamps `time.Time` and confirmed durations `time.Duration`, which encode as seconds in JSON. Values whose unit is not yet known are plain integers, and their documentation says so.

For anything without a typed function, `Client.Call`, `CallJSON`, `Property` and `Ubus` reach the router directly, and `Client.Events` streams its events. `discovery.Scan` finds units on the LAN.

## Status

The read commands have been tested against real hardware. The write commands have not been tested yet. They show what they will change and ask before doing it, but treat each one as untested until it has been tried on your own network.

## Layout

```text
*.go                   session: Dial, Client, Call, Property, Events, MAC, Role
device/                the unit the session is connected to
mesh/                  mesh points, registered units, ports, pairing
clients/               clients on the network
wifi/                  Wi-Fi networks, radios, channels, scan, WPS
network/               WAN, LAN, interfaces, UPnP, QoS
dhcp/                  DHCP pools and static leases
portforward/           port forwarding rules
access/                scheduled access control
teleport/              Teleport remote access
firmware/              mesh-wide firmware updates
system/                site, admin password, speed test, iperf, support, HomeKit
discovery/             UDP discovery on port 10001
internal/rpc           helpers shared by the area packages
internal/wire          JSON envelopes and msgpack codec
internal/srp           SRP-6a login
internal/secretstream  session encryption
internal/creds         saved logins for the CLI
internal/cli           CLI commands
internal/appinfo       build version, set by the Makefile
cmd/amplifi            CLI entry point
```

## Releasing

Push a version tag. The release workflow runs the checks and tests, builds the tarballs, `.deb` packages and `SHA256SUMS` with `make dist`, and publishes a GitHub release listing the commits since the previous tag. Tags with a suffix, such as `v1.2.0-rc.1`, become pre-releases.

```sh
git tag -a v1.2.0 -m "v1.2.0"
git push origin v1.2.0
```

`make dist VERSION=v1.2.0` builds the same files locally into `dist/`. It needs [nfpm](https://nfpm.goreleaser.com).

## License

Apache License 2.0. See [LICENSE](LICENSE).
