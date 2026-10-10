package cli

import (
	"fmt"

	"github.com/borud/amplifi/device"
)

type rebootCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run reboots the unit after confirmation.
func (c *rebootCmd) Run(opt *Options) error {
	return c.run(opt, c.yesFlag, fmt.Sprintf("reboot %s now?", c.Host), device.Reboot)
}
