package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/system"
)

type systemSupportCmd struct {
	Generate systemSupportGenerateCmd `kong:"cmd,help='build a support bundle (logs and configuration) on the unit'"`
	Download systemSupportDownloadCmd `kong:"cmd,help='build a support bundle and download it'"`
}

// systemSupportTimeout is how long the unit may take to build a bundle. It
// can take up to two minutes.
const systemSupportTimeout = 2 * time.Minute

type systemSupportGenerateCmd struct {
	unitFlags `kong:"embed"`
}

// Run builds a support bundle and prints its name, size and checksum.
func (c *systemSupportGenerateCmd) Run(opt *Options) error {
	return c.session(opt, systemSupportTimeout, func(ctx context.Context, cl *amplifi.Client) error {
		f, err := system.CreateSupportBundle(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(f)
		}
		return printPairs(os.Stdout, [][2]string{
			{"file", f.File},
			{"size", strconv.FormatInt(f.Size, 10)},
			{"md5", f.MD5Sum},
		})
	})
}

type systemSupportDownloadCmd struct {
	unitFlags `kong:"embed"`
	File      string        `kong:"short='f',help='where to save the bundle (default: its name on the unit, in the current directory)'"`
	Wait      time.Duration `kong:"help='maximum time for building and downloading',default='5m'"`
}

// Run builds a support bundle and saves it locally.
func (c *systemSupportDownloadCmd) Run(opt *Options) error {
	return c.session(opt, c.Wait, func(ctx context.Context, cl *amplifi.Client) error {
		fmt.Fprintln(os.Stderr, "building support bundle...")
		f, err := system.CreateSupportBundle(ctx, cl)
		if err != nil {
			return err
		}

		path := c.File
		if path == "" {
			path = filepath.Base(f.File)
		}
		out, err := os.Create(path)
		if err != nil {
			return err
		}
		defer out.Close()

		fmt.Fprintf(os.Stderr, "downloading %s (%d bytes)...\n", f.File, f.Size)
		if err := system.DownloadSupportBundle(ctx, cl, f, out); err != nil {
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		fmt.Printf("saved %s\n", path)
		return nil
	})
}
