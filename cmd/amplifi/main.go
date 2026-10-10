// Command amplifi inspects and controls AmpliFi routers from the command line.
package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/borud/amplifi/internal/appinfo"
	"github.com/borud/amplifi/internal/cli"
)

// opt are the parsed command line options.
var opt cli.Options

func main() {
	ktx := kong.Parse(&opt,
		kong.Description("Inspect and control AmpliFi routers over the local control protocol."),
		kong.UsageOnError(),
		kong.Vars{"version": appinfo.Version},
		kong.ConfigureHelp(kong.HelpOptions{
			Compact:             true,
			NoExpandSubcommands: true,
		}),
	)

	cli.ResolveHost(ktx)

	if err := ktx.Run(&opt); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
