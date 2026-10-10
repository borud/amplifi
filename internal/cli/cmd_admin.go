package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/system"
)

type adminCmd struct {
	Password adminPasswordCmd `kong:"cmd,help='change the admin password of the mesh'"`
}

type adminPasswordCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run changes the admin password, prompting for the new one twice.
func (c *adminPasswordCmd) Run(opt *Options) error {
	password, err := promptPassword("new admin password")
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("empty password")
	}
	again, err := promptPassword("repeat new admin password")
	if err != nil {
		return err
	}
	if again != password {
		return errors.New("passwords do not match")
	}

	if !c.approve(fmt.Sprintf("change the admin password of the mesh %s belongs to?", c.Host)) {
		return nil
	}
	err = c.commit(opt, func(ctx context.Context, cl *amplifi.Client) error {
		return system.SetAdminPassword(ctx, cl, password)
	})
	if err != nil {
		return err
	}
	fmt.Printf("saved logins for this mesh are now invalid; run 'amplifi login %s' again\n", c.Host)
	return nil
}
