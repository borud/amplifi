package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"slices"

	"github.com/borud/amplifi"
)

type watchCmd struct {
	unitFlags `kong:"embed"`
	Iface     []string `kong:"help='only show events from these interfaces, e.g. ClusterNode,Device'"`
	Event     []string `kong:"help='only show these events, e.g. ClientMacDiscovered'"`
	Exclude   []string `kong:"help='hide these events; the default hides the once-a-second port statistics',default='EthernetPortInfoChanged'"`
	All       bool     `kong:"help='show every event, ignoring --exclude'"`
}

// Run prints events from the unit until interrupted.
func (c *watchCmd) Run(opt *Options) error {
	log := opt.Logger()

	dialCtx, cancel := c.context()
	defer cancel()
	cl, err := c.dial(dialCtx, log)
	if err != nil {
		return err
	}
	defer cl.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	log.Info("watching for events; press Ctrl-C to stop", "unit", c.Host)

	for ev, err := range cl.Events(ctx) {
		if err != nil {
			return err
		}
		if !c.wants(ev) {
			continue
		}
		if err := printEvent(opt.JSON(), ev); err != nil {
			return err
		}
	}
	return nil
}

func (c *watchCmd) wants(ev amplifi.Event) bool {
	if len(c.Iface) > 0 && !slices.Contains(c.Iface, ev.Interface) {
		return false
	}
	if !c.All && slices.Contains(c.Exclude, ev.Name) && !slices.Contains(c.Event, ev.Name) {
		return false
	}
	return len(c.Event) == 0 || slices.Contains(c.Event, ev.Name)
}

func printEvent(asJSON bool, ev amplifi.Event) error {
	payload := readable(ev.Payload)

	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"time":      ev.Time,
			"interface": ev.Interface,
			"event":     ev.Name,
			"payload":   payload,
		})
	}

	line := fmt.Sprintf("%s  %s.%s", ev.Time.Format("15:04:05"), ev.Interface, ev.Name)
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		line += "  " + string(b)
	}
	fmt.Println(line)
	return nil
}
