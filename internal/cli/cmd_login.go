package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/device"
	"github.com/borud/amplifi/discovery"
	"github.com/borud/amplifi/internal/creds"
)

// probeTimeout bounds the discovery probe used to learn a unit's name and MAC.
const probeTimeout = time.Second

type loginCmd struct {
	Host     string        `kong:"arg,help='unit name (from a saved login), IP address or hostname'"`
	Password string        `kong:"help='admin password (prompted if omitted)',env='AMPLIFI_PASSWORD'"`
	Timeout  time.Duration `kong:"help='command timeout',default='15s'"`
	Default  bool          `kong:"help='make this the default unit'"`
	Replace  bool          `kong:"help='accept a certificate that differs from the saved one without asking'"`
}

// Run logs in with the password and saves the resulting password hash and the
// unit's certificate fingerprint, so later commands can reach the unit by
// name without a password and notice if it is replaced. The first unit saved
// becomes the default for commands run without --host.
func (c *loginCmd) Run(opt *Options) error {
	log := opt.Logger()
	u := &unitFlags{Host: c.Host, Password: c.Password, Timeout: c.Timeout}

	store, err := loadStore()
	if err != nil {
		return err
	}
	ip, saved, err := c.resolve(store)
	if err != nil {
		return err
	}
	if u.Password == "" {
		if u.Password, err = promptPassword("admin password"); err != nil {
			return err
		}
	}

	cl, err := c.dial(u, log, ip, saved)
	if err != nil {
		return err
	}
	defer cl.Close()

	ctx, cancel := u.context()
	defer cancel()
	info, err := device.Board(ctx, cl)
	if err != nil {
		return fmt.Errorf("encrypted channel check failed: %w", err)
	}

	unit := creds.Unit{IP: ip, Hash: cl.PasswordHash(), Fingerprint: cl.CertificateFingerprint(), Saved: time.Now()}
	pctx, cancelProbe := context.WithTimeout(ctx, probeTimeout)
	defer cancelProbe()
	found, err := discovery.Probe(pctx, ip)
	if err != nil {
		log.Warn("could not learn unit name; it can only be addressed by IP", "err", err)
	}
	if found != nil {
		unit.Name = found.HostName
		if found.MAC != nil {
			unit.MAC = found.MAC.String()
		}
	}

	store.Put(unit)
	if c.Default {
		store.SetDefault(unit)
	}
	if err := store.Save(); err != nil {
		return err
	}

	note := ""
	if store.IsDefault(unit) {
		note = " (default unit)"
	}
	fmt.Printf("logged in to %s (%s, %s)%s; saved to %s\n", displayName(unit), ip, info.Name, note, store.Path())
	return nil
}

// resolve returns the IP to log in to and the saved login for that IP, or a
// zero Unit if there is none. A saved name resolves to its saved IP.
func (c *loginCmd) resolve(store *creds.Store) (string, creds.Unit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	host := c.Host
	if saved, ok := store.Find(c.Host); ok {
		host = saved.IP
	}
	ip, err := resolveIP(ctx, host)
	if err != nil {
		return "", creds.Unit{}, err
	}
	saved, _ := store.Find(ip)
	return ip, saved, nil
}

// dial logs in to ip with the password. The certificate must match the saved
// fingerprint, if any. On a mismatch the user is asked whether to accept the
// new certificate, unless --replace was given.
func (c *loginCmd) dial(u *unitFlags, log *slog.Logger, ip string, saved creds.Unit) (*amplifi.Client, error) {
	pin := saved.Fingerprint
	if c.Replace {
		pin = ""
	}
	cl, err := dialPinned(u, log, ip, pin)
	mismatch, ok := errors.AsType[*amplifi.CertificateMismatchError](err)
	if !ok {
		return cl, err
	}

	fmt.Fprintf(os.Stderr, "warning: %s\n", mismatchReport(saved, mismatch))
	if !confirm("replace the saved certificate and log in?") {
		return nil, errors.New("login aborted; saved certificate kept")
	}
	return dialPinned(u, log, ip, mismatch.Got)
}

// dialPinned logs in to ip with the password, requiring the certificate
// fingerprint pin unless it is empty. It gets its own timeout, as it may
// follow a prompt.
func dialPinned(u *unitFlags, log *slog.Logger, ip, pin string) (*amplifi.Client, error) {
	ctx, cancel := u.context()
	defer cancel()

	opts := []amplifi.Option{amplifi.WithLogger(log)}
	if pin != "" {
		opts = append(opts, amplifi.WithCertificateFingerprint(pin))
	}
	return u.dialPassword(ctx, ip, opts...)
}

func displayName(u creds.Unit) string {
	if u.Name == "" {
		return u.IP
	}
	return u.Name
}
