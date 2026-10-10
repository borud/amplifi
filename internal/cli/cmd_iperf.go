package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/system"
)

type systemIperfCmd struct {
	unitFlags `kong:"embed"`
	Port      int `kong:"help='port for the iperf server (default: the iperf default)'"`
}

// Run starts an iperf server on the unit for 30 seconds.
func (c *systemIperfCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		res, err := system.RunIperf(ctx, cl, c.Port)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(res)
		}

		if res != nil {
			fmt.Printf("router answered: %s\n", inlineValue(res))
		}
		client := "iperf -c " + c.Host
		if c.Port > 0 {
			client += " -p " + strconv.Itoa(c.Port)
		}
		fmt.Printf("iperf server listening on %s for 30 seconds; from another machine run:\n  %s\n", c.Host, client)
		return nil
	})
}
