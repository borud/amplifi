package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/creds"
	"golang.org/x/term"
)

// unitFlags selects a unit and how to authenticate to it. Commands that talk
// to a unit embed it.
type unitFlags struct {
	Host     string        `kong:"short='H',help='unit name (from a saved login), IP address or hostname (default: the default unit)',env='AMPLIFI_HOST'"`
	Password string        `kong:"help='admin password (prompted if needed)',env='AMPLIFI_PASSWORD'"`
	Timeout  time.Duration `kong:"help='command timeout',default='15s'"`

	// saved is the default unit's login when --host was not given.
	saved *creds.Unit
}

// hostResolver is implemented by every command that embeds unitFlags.
type hostResolver interface {
	resolveHost()
}

// ResolveHost fills in --host from the default saved login for the selected
// command when it was not given, so commands can show and use the unit's
// name. Kong does not run hooks on embedded structs, so main calls this after
// parsing. A missing store or default is left for dial to report.
func ResolveHost(ktx *kong.Context) {
	node := ktx.Selected()
	if node == nil || !node.Target.CanAddr() {
		return
	}
	if r, ok := node.Target.Addr().Interface().(hostResolver); ok {
		r.resolveHost()
	}
}

// resolveHost keeps the default unit's login itself, not just its name, as
// several saved units may share a name.
func (u *unitFlags) resolveHost() {
	if u.Host != "" {
		return
	}
	store, err := loadStore()
	if err != nil {
		return
	}
	if d, ok := store.DefaultUnit(); ok {
		u.Host = displayName(d)
		u.saved = &d
	}
}

func (u *unitFlags) context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), u.Timeout)
}

// dial connects to the unit, logging in with its saved password hash when
// there is one and with the password otherwise. A rejected saved login falls
// back to the password. A saved unit's certificate must match its saved
// fingerprint; one saved without a fingerprint is pinned now. The caller must
// Close the returned client.
func (u *unitFlags) dial(ctx context.Context, log *slog.Logger) (*amplifi.Client, error) {
	if u.Host == "" {
		return nil, errors.New("no unit given: use --host, or run 'amplifi login <host>' once to save a default")
	}

	saved, ok := u.savedLogin(log)
	if !ok {
		return u.dialPassword(ctx, u.Host, amplifi.WithLogger(log))
	}

	cl, err := u.dialSaved(ctx, log, saved)
	if err != nil {
		return nil, certChanged(saved, err)
	}
	if saved.Fingerprint == "" {
		pinFirstUse(log, saved, cl.CertificateFingerprint())
	}
	return cl, nil
}

// dialSaved logs in to a saved unit, pinning its saved fingerprint.
func (u *unitFlags) dialSaved(ctx context.Context, log *slog.Logger, saved creds.Unit) (*amplifi.Client, error) {
	opts := []amplifi.Option{amplifi.WithLogger(log)}
	if saved.Fingerprint != "" {
		opts = append(opts, amplifi.WithCertificateFingerprint(saved.Fingerprint))
	}
	if u.Password != "" {
		return u.dialPassword(ctx, saved.IP, opts...)
	}

	cl, err := amplifi.DialWithHash(ctx, saved.IP, saved.Hash, opts...)
	if !errors.Is(err, amplifi.ErrAuth) {
		return cl, err
	}
	log.Warn("saved login rejected; run 'amplifi login' to update it", "unit", u.Host)
	return u.dialPassword(ctx, saved.IP, opts...)
}

// certChangedError explains a certificate mismatch to the user.
type certChangedError struct {
	saved    creds.Unit
	mismatch *amplifi.CertificateMismatchError
}

func (e *certChangedError) Error() string {
	return mismatchReport(e.saved, e.mismatch) +
		"\nIf you expect the change, run 'amplifi login " + displayName(e.saved) + "' again."
}

func (e *certChangedError) Unwrap() error { return e.mismatch }

// certChanged wraps a certificate mismatch in a certChangedError and returns
// other errors as they are.
func certChanged(saved creds.Unit, err error) error {
	mismatch, ok := errors.AsType[*amplifi.CertificateMismatchError](err)
	if !ok {
		return err
	}
	return &certChangedError{saved: saved, mismatch: mismatch}
}

// mismatchReport describes a changed certificate.
func mismatchReport(saved creds.Unit, mismatch *amplifi.CertificateMismatchError) string {
	return fmt.Sprintf("certificate for %s (%s) has changed\n"+
		"  saved:   %s\n"+
		"  current: %s\n"+
		"This can mean someone is intercepting the connection, or the unit was\n"+
		"reset or upgraded.",
		displayName(saved), saved.IP, mismatch.Want, mismatch.Got)
}

// pinFirstUse saves the fingerprint of a unit saved without one. Failing to
// save is not worth failing the command for.
func pinFirstUse(log *slog.Logger, saved creds.Unit, fp string) {
	store, err := loadStore()
	if err != nil {
		log.Debug("not saving certificate fingerprint", "err", err)
		return
	}
	saved.Fingerprint = fp
	store.Put(saved)
	if err := store.Save(); err != nil {
		log.Debug("not saving certificate fingerprint", "err", err)
		return
	}
	log.Debug("saved certificate fingerprint", "unit", displayName(saved), "fingerprint", fp)
}

// savedLogin returns the saved login of the unit: the default unit when
// --host was not given, else the one --host names.
func (u *unitFlags) savedLogin(log *slog.Logger) (creds.Unit, bool) {
	if u.saved != nil {
		return *u.saved, true
	}
	return lookup(log, u.Host)
}

// dialPassword logs in to host with --password, prompting if it is unset.
func (u *unitFlags) dialPassword(ctx context.Context, host string, opts ...amplifi.Option) (*amplifi.Client, error) {
	password := u.Password
	if password == "" {
		var err error
		password, err = promptPassword("admin password")
		if err != nil {
			return nil, err
		}
	}
	return amplifi.Dial(ctx, host, password, opts...)
}

// lookup finds a saved login. A store that cannot be read is reported and
// treated as empty, so a broken file never blocks password login.
func lookup(log *slog.Logger, key string) (creds.Unit, bool) {
	store, err := loadStore()
	if err != nil {
		log.Warn("ignoring saved logins", "err", err)
		return creds.Unit{}, false
	}
	return store.Find(key)
}

func loadStore() (*creds.Store, error) {
	path, err := creds.DefaultPath()
	if err != nil {
		return nil, err
	}
	return creds.Load(path)
}

// resolveIP returns host as an IP address, resolving hostnames.
func resolveIP(ctx context.Context, host string) (string, error) {
	if net.ParseIP(host) != nil {
		return host, nil
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", host, err)
	}
	return addrs[0], nil
}

// promptPassword reads a password from the terminal without echo.
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt+": ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(b), nil
}

// session dials the unit, runs fn and closes the session. extra extends the
// command timeout for long-running commands.
func (u *unitFlags) session(opt *Options, extra time.Duration, fn func(context.Context, *amplifi.Client) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), u.Timeout+extra)
	defer cancel()

	cl, err := u.dial(ctx, opt.Logger())
	if err != nil {
		return err
	}
	defer cl.Close()

	return fn(ctx, cl)
}

// writeFunc makes a write command's change on the unit.
type writeFunc func(context.Context, *amplifi.Client) error

// apply is the shape of a write command that depends on the unit's current
// state: read it, let plan record the changes and return the write that
// makes them, show the changes and ask, then write and print "done".
//
// The write gets a fresh timeout on the same session. The session's timeout
// kept running while the prompt waited for an answer, and a slow answer
// must not make the write time out.
func apply[T any](
	opt *Options, u *unitFlags, y yesFlag, prompt string,
	read func(context.Context, *amplifi.Client) (T, error),
	plan func(*changeSet, T) writeFunc,
) error {
	return u.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		cur, err := read(ctx, cl)
		if err != nil {
			return err
		}

		var cs changeSet
		write := plan(&cs, cur)
		ok, err := cs.approve(y, prompt)
		if err != nil || !ok {
			return err
		}

		wctx, cancel := u.context()
		defer cancel()
		if err := write(wctx, cl); err != nil {
			return err
		}
		fmt.Println("done")
		return nil
	})
}

// run is the shape of a write command that needs nothing from the unit
// before asking: ask, then commit the write.
func (u *unitFlags) run(opt *Options, y yesFlag, prompt string, write writeFunc) error {
	if !y.approve(prompt) {
		return nil
	}
	return u.commit(opt, write)
}

// commit makes an approved write on a new session and prints "done".
func (u *unitFlags) commit(opt *Options, write writeFunc) error {
	if err := u.session(opt, 0, write); err != nil {
		return err
	}
	fmt.Println("done")
	return nil
}

// yesFlag is embedded by commands that change something and ask first.
type yesFlag struct {
	Yes bool `kong:"short='y',help='do not ask for confirmation'"`
}

// approve asks prompt unless --yes was given, printing "aborted" on a no.
func (y yesFlag) approve(prompt string) bool {
	if y.Yes || confirm(prompt) {
		return true
	}
	fmt.Println("aborted")
	return false
}

// confirm asks a y/N question on stderr and reads the answer from stdin.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	answer := strings.ToLower(readLine())
	return answer == "y" || answer == "yes"
}

// confirmName asks the user to type name to confirm an irreversible action
// on it, unless --yes was given, printing "aborted" on a mismatch.
func (y yesFlag) confirmName(name string) bool {
	if y.Yes {
		return true
	}
	fmt.Fprintf(os.Stderr, "this erases ALL settings on %s and cannot be undone.\ntype %q to confirm: ", name, name)
	if readLine() == name {
		return true
	}
	fmt.Println("aborted")
	return false
}

// readLine reads one line from stdin without surrounding space, or "" when
// stdin cannot be read.
func readLine() string {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}

// sleepCtx waits for d or until ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// parseMACs parses MAC address arguments.
func parseMACs(args []string) ([]amplifi.MAC, error) {
	macs := make([]amplifi.MAC, 0, len(args))
	for _, a := range args {
		mac, err := amplifi.ParseMAC(a)
		if err != nil {
			return nil, err
		}
		macs = append(macs, mac)
	}
	return macs, nil
}
