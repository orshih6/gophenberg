// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gopherium/framework/goncierge"
	"github.com/gopherium/framework/gonsole"
	accounts "github.com/gopherium/framework/gonsole/auth"
	accountsqlite "github.com/gopherium/framework/gonsole/auth/sqlite"
	authkitpg "github.com/gopherium/gouncer/authkit/postgres"
	"github.com/gopherium/gouncer/avatar"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gopherium/gophenberg/internal/content"
	"github.com/gopherium/gophenberg/internal/media"
	"github.com/gopherium/gophenberg/internal/mediahost"
	"github.com/gopherium/gophenberg/internal/postgres"
	"github.com/gopherium/gophenberg/internal/sqlite"
	"github.com/gopherium/gophenberg/sdk"
)

// composeConfig carries the values compose reads.
type composeConfig struct {
	databaseURL string
	handle      *sql.DB
	fieldDepth  int
	mediaDir    string
	getenv      func(string) string
	roles       *goncierge.Registry
}

// site is what compose builds over one pool or one SQLite handle.
type site struct {
	pool       *pgxpool.Pool
	users      *authkitpg.UserStore
	content    *postgres.ContentStore
	types      *postgres.TypeStore
	registry   *content.Registry
	saved      mediahost.Saved
	library    media.Store
	settings   content.SiteSettingStore
	readers    content.UserSettingStore
	release    func()
	registered []sdk.Plugin
	failed     error
	lane       *pluginLane
	roles      *goncierge.Registry
	avatars    avatar.Options
}

// composeOf returns the values compose reads out of the server settings.
func composeOf(settings runConfig, getenv func(string) string) composeConfig {
	return composeConfig{
		databaseURL: settings.databaseURL, fieldDepth: settings.fieldDepth, mediaDir: settings.mediaDir, getenv: getenv,
	}
}

// openStores builds the stores and the registry of the engine the address picks, migrating nothing.
func openStores(ctx context.Context, cfg composeConfig) (site, error) {
	if onSQLite(cfg.databaseURL) {
		return sqliteStores(cfg), nil
	}
	return postgresStores(ctx, cfg)
}

// postgresStores builds the pool, every store over it and the registry, releasing the pool on close.
func postgresStores(ctx context.Context, cfg composeConfig) (site, error) {
	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		return site{}, fmt.Errorf("parse database url: %w", err)
	}
	store := postgres.NewMediaStore(pool)
	built := site{
		pool:     pool,
		users:    authkitpg.NewUserStore(pool),
		content:  postgres.NewContentStore(pool),
		types:    postgres.NewTypeStore(pool),
		saved:    store.Saved,
		library:  libraryOf(store, cfg.mediaDir),
		settings: postgres.NewSettingStore(pool),
		readers:  postgres.NewUserSettingStore(pool),
		release:  pool.Close,
	}
	built.registry = registryFrom(cfg.fieldDepth, built.types)
	return built, nil
}

// sqliteStores builds the media and setting stores over the SQLite handle, leaving users, content and types nil.
func sqliteStores(cfg composeConfig) site {
	store := sqlite.NewMediaStore(cfg.handle)
	return site{
		registry: registryFrom(cfg.fieldDepth, nil),
		saved:    store.Saved,
		library:  libraryOf(store, cfg.mediaDir),
		settings: sqlite.NewSettingStore(cfg.handle),
		readers:  sqlite.NewUserSettingStore(cfg.handle),
		release:  func() {},
	}
}

// libraryOf returns the media store the site lends while a media folder is set, nil without one.
func libraryOf(store media.Store, mediaDir string) media.Store {
	if mediaDir == "" {
		return nil
	}
	return store
}

// handleOpener returns the opener of a run's one SQLite handle under the SQLite settings, nil on any other address.
func handleOpener(getenv func(string) string) func(context.Context, string) (*sql.DB, error) {
	env := settingsEnv(getenv)
	if !onSQLite(env.Value("DATABASE_URL")) {
		return nil
	}
	return func(_ context.Context, address string) (*sql.DB, error) {
		if err := checkSQLitePath(env, address); err != nil {
			return nil, err
		}
		settings, err := sqlite.SettingsFrom(env)
		if err != nil {
			return nil, err
		}
		return sqlite.Open(address, settings)
	}
}

// migrations returns the schema steps of the engine the database address getenv names picks, in the order they apply.
func migrations(getenv func(string) string) []gonsole.Step {
	env := settingsEnv(getenv)
	if onSQLite(env.Value("DATABASE_URL")) {
		return sqliteMigrations(env)
	}
	return postgresMigrations()
}

// postgresMigrations returns the steps a PostgreSQL database takes at its address, in the order they apply.
func postgresMigrations() []gonsole.Step {
	return []gonsole.Step{accounts.Migration(), accounts.RecordMigration(), {Name: "core", Run: postgres.Migrate}}
}

// sqliteMigrations returns the steps a SQLite database takes on the run's one handle, in the order they apply.
func sqliteMigrations(env gonsole.Env) []gonsole.Step {
	return []gonsole.Step{
		sqliteStep("accounts", env, accountStep(accountsqlite.Migration)),
		sqliteStep("records", env, accountStep(accountsqlite.RecordMigration)),
		sqliteStep("core", env, sqlite.Migrate),
	}
}

// sqliteStep returns the step named name that reads the migration lock settings from env, then applies on the handle.
func sqliteStep(
	name string, env gonsole.Env, apply func(context.Context, *sql.DB, sqlite.MigrateOptions) error,
) gonsole.Step {
	return gonsole.Step{Name: name, RunOn: func(ctx context.Context, db *sql.DB) error {
		opts, err := sqlite.MigrateOptionsFrom(env)
		if err != nil {
			return err
		}
		return apply(ctx, db, opts)
	}}
}

// accountStep returns the run on a handle of the account schema step build makes under the lock settings it is handed.
func accountStep(
	build func(accountsqlite.MigrateOptions) gonsole.Step,
) func(context.Context, *sql.DB, sqlite.MigrateOptions) error {
	return func(ctx context.Context, db *sql.DB, opts sqlite.MigrateOptions) error {
		return build(accountsqlite.MigrateOptions(opts)).RunOn(ctx, db)
	}
}

// migrate applies every PostgreSQL schema step to the database at databaseURL.
func migrate(ctx context.Context, databaseURL string) error {
	for _, step := range postgresMigrations() {
		if err := step.Run(ctx, databaseURL); err != nil {
			return err
		}
	}
	return nil
}
