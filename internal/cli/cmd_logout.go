package cli

import "fmt"

type logoutCmd struct {
	Host string `kong:"arg,help='unit name, IP address or MAC address of a saved login'"`
}

// Run deletes a saved login.
func (c *logoutCmd) Run(*Options) error {
	store, err := loadStore()
	if err != nil {
		return err
	}
	if !store.Remove(c.Host) {
		return fmt.Errorf("no saved login for %q", c.Host)
	}
	if err := store.Save(); err != nil {
		return err
	}
	fmt.Printf("removed saved login for %s\n", c.Host)
	return nil
}
