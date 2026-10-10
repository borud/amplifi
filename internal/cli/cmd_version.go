package cli

import (
	"fmt"

	"github.com/borud/amplifi/internal/appinfo"
)

type versionCmd struct{}

// Run prints the build information.
func (c *versionCmd) Run(opt *Options) error {
	if opt.JSON() {
		return printJSON(map[string]string{
			"version":   appinfo.Version,
			"commit":    appinfo.Commit,
			"buildTime": appinfo.BuildTime,
		})
	}
	fmt.Printf("version:    %s\ncommit:     %s\nbuild time: %s\n", appinfo.Version, appinfo.Commit, appinfo.BuildTime)
	return nil
}
