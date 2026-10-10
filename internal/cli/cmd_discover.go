package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/borud/amplifi/discovery"
)

type discoverCmd struct {
	Timeout time.Duration `kong:"help='how long to wait for replies',default='3s'"`
}

// Run lists the units that answer a discovery broadcast.
func (c *discoverCmd) Run(opt *Options) error {
	log := opt.Logger()
	log.Debug("discovering", "timeout", c.Timeout)

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	units, err := discovery.Scan(ctx)
	if err != nil {
		return err
	}

	if opt.JSON() {
		return printJSON(units)
	}
	if len(units) == 0 {
		fmt.Println("no units found")
		return nil
	}

	tw := newTable(os.Stdout)
	fmt.Fprintln(tw, "NAME\tIP\tMODEL\tFIRMWARE\tMAC")
	for _, u := range units {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", orDash(u.HostName), u.IP, u.Model, u.Firmware, u.MAC)
	}
	return tw.Flush()
}
