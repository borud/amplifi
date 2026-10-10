package system

import (
	"context"
	"errors"
	"fmt"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// Users config option codes (UserConfigOption) and the admin role
// (UserRole.Admin).
const (
	adminOptRole     = "2"
	adminOptPassword = "3"
	adminRoleAdmin   = 1
)

// SetAdminPassword changes the admin password of the whole mesh
// (ClusterNode.SetUsersConfig). The router
// derives the SRP verifier itself. Hashes from amplifi.PasswordHash become
// invalid.
func SetAdminPassword(ctx context.Context, c *amplifi.Client, password string) error {
	if password == "" {
		return errors.New("set admin password: empty password")
	}
	if err := rpc.SetCluster(ctx, c, "SetUsersConfig", adminUsersPayload(password)); err != nil {
		return fmt.Errorf("set admin password: %w", err)
	}
	return nil
}

// adminUsersPayload is the users map of packSetUsersConfigCall:
// {"admin": {Role: Admin, Password: password}}.
func adminUsersPayload(password string) map[string]any {
	return map[string]any{
		"admin": map[string]any{
			adminOptRole:     adminRoleAdmin,
			adminOptPassword: password,
		},
	}
}
