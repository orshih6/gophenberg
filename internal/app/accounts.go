// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"time"

	"github.com/gopherium/framework/goncierge"
	"github.com/gopherium/framework/gonsole"
	accounts "github.com/gopherium/framework/gonsole/auth"

	"github.com/gopherium/gophenberg/internal/role"
)

// recordTimeout bounds storing one command record when COMMAND_RECORD_TIMEOUT names no other.
const recordTimeout = 5 * time.Second

// recordsLimit is how many records account:records lists when COMMAND_RECORDS_LIMIT names no other.
const recordsLimit = 50

// errAccountsOnSQLite answers every account command and every check on the acting account on a SQLite database.
var errAccountsOnSQLite = fmt.Errorf("account commands: %w", errPostgresOnly)

// accountConfig returns the configuration of the account commands and of the check on the acting account over registry.
func accountConfig(registry *goncierge.Registry, getenv func(string) string) accounts.Config {
	return accounts.Config{
		Roles:         rolesHook(registry, getenv),
		Capability:    string(role.ManageUsers),
		RecordTimeout: recordTimeout,
		RecordsLimit:  recordsLimit,
		Stores:        accountStores(getenv),
	}
}

// accountStores returns the hook answering errAccountsOnSQLite when getenv names a sqlite: address, nil on any other.
func accountStores(
	getenv func(string) string,
) func(context.Context, gonsole.Call) (accounts.Stores, func(context.Context) error, error) {
	if !onSQLite(settingsEnv(getenv).Value("DATABASE_URL")) {
		return nil
	}
	return func(context.Context, gonsole.Call) (accounts.Stores, func(context.Context) error, error) {
		return accounts.Stores{}, nil, errAccountsOnSQLite
	}
}

// rolesHook returns declaredRoles over registry, or on a sqlite: address the hook answering errAccountsOnSQLite.
func rolesHook(
	registry *goncierge.Registry, getenv func(string) string,
) func(context.Context, gonsole.Call) (accounts.Roles, error) {
	if !onSQLite(settingsEnv(getenv).Value("DATABASE_URL")) {
		return declaredRoles(registry)
	}
	return func(context.Context, gonsole.Call) (accounts.Roles, error) {
		return accounts.Roles{}, errAccountsOnSQLite
	}
}

// declaredRoles returns the hook answering the roles the core and the call's plugins declared into registry.
func declaredRoles(registry *goncierge.Registry) func(context.Context, gonsole.Call) (accounts.Roles, error) {
	return func(ctx context.Context, call gonsole.Call) (accounts.Roles, error) {
		loaded, err := call.Plugins(ctx)
		if err != nil {
			return accounts.Roles{}, err
		}
		if loaded.Failed != nil {
			return accounts.Roles{}, fmt.Errorf("register plugins: %w", loaded.Failed)
		}
		return accountRoles(registry), nil
	}
}

// accountRoles returns the roles registry holds, privileged where they carry manage_users.
func accountRoles(registry *goncierge.Registry) accounts.Roles {
	return accounts.RolesFrom(registry, registry.HoldersOf(string(role.ManageUsers)))
}
