package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/system"
)

type systemHomeKitCmd struct {
	Show    systemHomeKitShowCmd    `kong:"cmd,help='show the HomeKit service state'"`
	Disable systemHomeKitDisableCmd `kong:"cmd,help='turn the HomeKit service off'"`
}

type systemHomeKitShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the HomeKit state.
func (c *systemHomeKitShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		h, err := system.GetHomeKit(ctx, cl)
		if err != nil {
			return homekitError(err)
		}
		if opt.JSON() {
			return printJSON(h)
		}

		return printPairs(os.Stdout, [][2]string{
			{"enabled", strconv.FormatBool(h.Enabled)},
			{"provisioned", strconv.FormatBool(h.Provisioned)},
			{"can run", strconv.FormatBool(h.CanRun)},
			{"can run now", strconv.FormatBool(h.CanRunNow)},
		})
	})
}

type systemHomeKitDisableCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run turns HomeKit off.
func (c *systemHomeKitDisableCmd) Run(opt *Options) error {
	return c.run(opt, c.yesFlag, fmt.Sprintf("disable HomeKit on %s?", c.Host), func(ctx context.Context, cl *amplifi.Client) error {
		return homekitError(system.DisableHomeKit(ctx, cl))
	})
}

// homekitError explains a refused HomeKit call: some firmware refuses the
// method.
func homekitError(err error) error {
	if amplifi.IsRouterError(err) {
		return fmt.Errorf("%w (not supported by this firmware)", err)
	}
	return err
}
