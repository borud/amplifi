package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/borud/amplifi"
)

type rawCmd struct {
	Call rawCallCmd `kong:"cmd,help='call any method and print the decoded result'"`
	Prop rawPropCmd `kong:"cmd,help='properties'"`
	Ubus rawUbusCmd `kong:"cmd,help='call an OpenWrt ubus method through the unit'"`
}

type rawCallCmd struct {
	unitFlags `kong:"embed"`
	Iface     string `kong:"arg,help='interface, e.g. Device, ClusterNode, FwUpdate, MagicLink, Unsecure or a full name'"`
	Method    string `kong:"arg,help='method name, e.g. GetSystemBoardInfo'"`

	Msgpack    string `kong:"help='argument as JSON, sent as msgpack (decimal object keys become integer option codes)',xor='arg'"`
	MsgpackHex string `kong:"help='argument as raw msgpack, hex encoded',xor='arg'"`
	Null       bool   `kong:"help='send a msgpack nil argument, as many getters expect',xor='arg'"`
	JSON       string `kong:"name='json',help='argument as JSON, sent as payload.value',xor='arg'"`
}

// Run calls the method and prints its result as JSON.
func (c *rawCallCmd) Run(opt *Options) error {
	ctx, cancel := c.context()
	defer cancel()

	cl, err := c.dial(ctx, opt.Logger())
	if err != nil {
		return err
	}
	defer cl.Close()

	var v any
	switch {
	case c.JSON != "":
		arg, err := parseJSONArg(c.JSON)
		if err != nil {
			return err
		}
		v, err = cl.CallJSON(ctx, c.Iface, c.Method, arg)
		if err != nil {
			return err
		}
	default:
		arg, err := c.msgpackArg()
		if err != nil {
			return err
		}
		v, err = cl.Call(ctx, c.Iface, c.Method, arg)
		if err != nil {
			return err
		}
	}
	return printJSON(readable(v))
}

func (c *rawCallCmd) msgpackArg() (any, error) {
	switch {
	case c.Null:
		return amplifi.RawMsgpack{0xc0}, nil
	case c.MsgpackHex != "":
		b, err := hex.DecodeString(c.MsgpackHex)
		if err != nil {
			return nil, fmt.Errorf("--msgpack-hex: %w", err)
		}
		return amplifi.RawMsgpack(b), nil
	case c.Msgpack != "":
		return parseJSONArg(c.Msgpack)
	default:
		return nil, nil
	}
}

type rawPropCmd struct {
	Show rawPropShowCmd `kong:"cmd,help='show a property as JSON'"`
	Set  rawPropSetCmd  `kong:"cmd,help='change a property'"`
}

type rawPropShowCmd struct {
	unitFlags `kong:"embed"`
	Iface     string `kong:"arg,help='interface, e.g. Device'"`
	Name      string `kong:"arg,help='property, e.g. FriendlyName'"`
}

// Run reads the property and prints it as JSON.
func (c *rawPropShowCmd) Run(opt *Options) error {
	ctx, cancel := c.context()
	defer cancel()

	cl, err := c.dial(ctx, opt.Logger())
	if err != nil {
		return err
	}
	defer cl.Close()

	v, err := cl.Property(ctx, c.Iface, c.Name)
	if err != nil {
		return err
	}
	return printJSON(readable(v))
}

type rawPropSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Iface     string `kong:"arg,help='interface, e.g. Device'"`
	Name      string `kong:"arg,help='property, e.g. FriendlyName'"`
	Value     string `kong:"arg,help='new value as JSON; quote strings, e.g. \"\\\"name\\\"\"'"`
}

// Run writes the property after confirmation.
func (c *rawPropSetCmd) Run(opt *Options) error {
	value, err := parseJSONArg(c.Value)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf("set %s.%s on %s to %s?", c.Iface, c.Name, c.Host, c.Value)
	return c.run(opt, c.yesFlag, prompt, func(ctx context.Context, cl *amplifi.Client) error {
		return cl.SetProperty(ctx, c.Iface, c.Name, value)
	})
}

type rawUbusCmd struct {
	unitFlags `kong:"embed"`
	Path      string `kong:"arg,help='ubus object, e.g. perfd'"`
	Method    string `kong:"arg,help='ubus method, e.g. mac-table'"`
	Args      string `kong:"arg,optional,help='arguments as a JSON object'"`
}

// Run calls the ubus method and prints its result as JSON.
func (c *rawUbusCmd) Run(opt *Options) error {
	args := map[string]any{}
	if c.Args != "" {
		if err := json.Unmarshal([]byte(c.Args), &args); err != nil {
			return fmt.Errorf("ubus args: %w", err)
		}
	}

	ctx, cancel := c.context()
	defer cancel()

	cl, err := c.dial(ctx, opt.Logger())
	if err != nil {
		return err
	}
	defer cl.Close()

	v, err := cl.Ubus(ctx, c.Path, c.Method, args)
	if err != nil {
		return err
	}
	return printJSON(v)
}
