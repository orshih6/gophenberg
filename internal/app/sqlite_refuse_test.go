// SPDX-License-Identifier: Apache-2.0

package app

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gopherium/framework/goncierge"
	"github.com/gopherium/framework/gonsole"
	"github.com/gopherium/framework/gonsole/testkit"

	"github.com/gopherium/gophenberg/sdk"
)

// errInputRead is what standard input answers every command that reads it.
var errInputRead = errors.New("standard input was read")

// migratedSQLiteFile returns the path of a SQLite file migrate built every owner's schema in.
func migratedSQLiteFile(t *testing.T) string {
	t.Helper()
	path := emptySQLiteFile(t)
	if got := testkit.Run(t, Program(migratingSQLite(path), noPlugins), "", "migrate"); got.Code != gonsole.ExitDone {
		t.Fatalf("migrate = %d with stderr %q, want 0", got.Code, got.Stderr)
	}
	return path
}

// runUnread runs args through program over a standard input that fails every read.
func runUnread(t *testing.T, program gonsole.Program, args ...string) testkit.Result {
	t.Helper()
	var stdout, stderr strings.Builder
	code := program.Run(t.Context(), args, iotest.ErrReader(errInputRead), &stdout, &stderr)
	return testkit.Result{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

func TestEveryCommandNeedingTheWholeSiteNamesTheSQLiteError(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args  []string
		named string
	}{
		"serve":        {[]string{"serve"}, "serve"},
		"seed -yes":    {[]string{"seed", "-yes"}, "seed"},
		"account:list": {[]string{"account:list"}, "account commands"},
		"account:create-admin": {
			[]string{"account:create-admin", "-email", "admin@example.com", "-name", "Holder", "-role", "admin"},
			"account commands",
		},
		"account:role as an admin": {
			[]string{"account:role", "author@example.com", "editor", "-yes", "-as", "admin@example.com"},
			"account commands",
		},
	}
	for testName, tc := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			program := Program(migratingSQLite(migratedSQLiteFile(t)), noPlugins)

			got := runUnread(t, program, tc.args...)

			want := "gophenberg: " + tc.named + ": " + errPostgresOnly.Error() + "\n"
			if got.Code != gonsole.ExitFailed || got.Stdout != "" || !strings.HasSuffix(got.Stderr, want) {
				t.Errorf("%q = %d, stdout %q, stderr %q, want 1, no password asked and %q last",
					tc.args, got.Code, got.Stdout, got.Stderr, want)
			}
		})
	}
}

func TestAccountCommandsActingAsAnAccountNameTheSQLiteErrorBeforeOpeningTheFile(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args     []string
		settings map[string]string
	}{
		"account:role": {
			[]string{"account:role", "author@example.com", "editor", "-yes", "-as", "admin@example.com"}, nil,
		},
		"account:disable": {
			[]string{"account:disable", "author@example.com", "-yes", "-as", "admin@example.com"}, nil,
		},
		"account:grant-role under a bad SQLite setting": {
			[]string{"account:grant-role", "-role", "editor", "-yes", "-as", "admin@example.com"},
			map[string]string{"GOPHENBERG_SQLITE_SYNCHRONOUS": "off"},
		},
	}
	for testName, tc := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "site.db")
			settings := map[string]string{"GOPHENBERG_DATABASE_URL": sqlitePrefix + path}
			maps.Copy(settings, tc.settings)

			got := runUnread(t, Program(testkit.Getenv(settings), noPlugins), tc.args...)

			want := "gophenberg: account commands: " + errPostgresOnly.Error() + "\n"
			if got.Code != gonsole.ExitFailed || got.Stdout != "" || !strings.HasSuffix(got.Stderr, want) {
				t.Errorf("%q = %d, stdout %q, stderr %q, want 1 and %q last",
					tc.args, got.Code, got.Stdout, got.Stderr, want)
			}
			if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the file after %q: %v, want it still missing", tc.args, err)
			}
		})
	}
}

func TestAPaddedSQLiteAddressPicksSQLiteForEveryCommand(t *testing.T) {
	t.Parallel()

	padded := func(path string) func(string) string {
		return testkit.Getenv(map[string]string{"GOPHENBERG_DATABASE_URL": "  " + sqlitePrefix + path + "  "})
	}
	onFile, onMissing := padded(emptySQLiteFile(t)), padded(filepath.Join(t.TempDir(), "site.db"))

	migrated := testkit.Run(t, Program(onFile, noPlugins), "", "migrate")

	if migrated.Code != gonsole.ExitDone || migrated.Stdout != migratedLines {
		t.Errorf("migrate = %d, stdout %q, stderr %q, want 0 and %q",
			migrated.Code, migrated.Stdout, migrated.Stderr, migratedLines)
	}
	for _, tc := range []struct {
		getenv func(string) string
		args   []string
		named  string
	}{
		{onFile, []string{"serve"}, "serve"},
		{onFile, []string{"account:list"}, "account commands"},
		{onMissing, []string{"account:role", "author@example.com", "editor", "-yes", "-as", "admin@example.com"},
			"account commands"},
	} {
		got := runUnread(t, Program(tc.getenv, noPlugins), tc.args...)
		if want := "gophenberg: " + tc.named + ": " + errPostgresOnly.Error() + "\n"; got.Code != gonsole.ExitFailed ||
			!strings.HasSuffix(got.Stderr, want) {
			t.Errorf("%q = %d with stderr %q, want 1 and %q last", tc.args, got.Code, got.Stderr, want)
		}
	}
}

func TestServeOnSQLiteAnswersBeforeTheSiteOrTheFileIsTouched(t *testing.T) {
	t.Parallel()

	path := emptySQLiteFile(t)
	registered := false
	plugins := func(sdk.Deps) ([]sdk.Plugin, error) {
		registered = true
		return nil, nil
	}

	got := testkit.Run(t, Program(migratingSQLite(path), plugins), "", "serve")

	if want := "serve: " + errPostgresOnly.Error(); got.Code != gonsole.ExitFailed ||
		!strings.Contains(got.Stderr, want) {
		t.Errorf("serve = %d with stderr %q, want 1 and %q", got.Code, got.Stderr, want)
	}
	if registered {
		t.Error("the plugins registered, want serve to answer before it builds the site")
	}
	if names := folderNames(t, filepath.Dir(path)); !slices.Equal(names, []string{"site.db"}) {
		t.Errorf("the folder holds %v after serve, want only site.db with no lock file", names)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("the file holds %d bytes after serve, want 0 with nothing migrated", info.Size())
	}
}

func TestSeedOnSQLiteMigratesTheFileThenNamesTheSQLiteError(t *testing.T) {
	t.Parallel()

	path := emptySQLiteFile(t)
	k := &keeper{}

	got := testkit.Run(t, Program(migratingSQLite(path), keeping(k)), "", "seed", "-yes")

	if want := "seed: " + errPostgresOnly.Error(); got.Code != gonsole.ExitFailed ||
		!strings.Contains(got.Stderr, want) {
		t.Errorf("seed -yes = %d with stderr %q, want 1 and %q", got.Code, got.Stderr, want)
	}
	if want := []string{"migrate", "stop"}; !slices.Equal(k.steps(), want) {
		t.Errorf("the plugin was asked %v, want %v with no demo data stored", k.steps(), want)
	}
	for table, versions := range appliedVersions(t, openSQLiteFile(t, path)) {
		if !appliedOne(versions) {
			t.Errorf("%s holds the versions %v after seed -yes, want the file migrated first", table, versions)
		}
	}
}

func TestAPostgreSQLProgramKeepsTheAccountStoresOfItsAddress(t *testing.T) {
	t.Parallel()

	getenv := testkit.Getenv(map[string]string{"GOPHENBERG_DATABASE_URL": unreachableDatabaseURL})

	if accountConfig(goncierge.New(), getenv).Stores != nil {
		t.Error("Stores is set on a PostgreSQL address, want nil so the account commands open their own pool")
	}
}

func TestAPostgreSQLAccountChangeStillReadsTheRolesOfTheRegisteredPlugins(t *testing.T) {
	t.Parallel()

	env := testkit.Getenv(map[string]string{"GOPHENBERG_DATABASE_URL": unreachableDatabaseURL})
	failing := func(sdk.Deps) ([]sdk.Plugin, error) { return nil, errors.New("plugin teams: a setting is missing") }

	got := testkit.Run(t, Program(env, failing), "",
		"account:role", "author@example.com", "editor", "-yes", "-as", "admin@example.com")

	if want := "register plugins: plugin teams: a setting is missing"; got.Code != gonsole.ExitFailed ||
		!strings.Contains(got.Stderr, want) {
		t.Errorf("account:role on PostgreSQL = %d with stderr %q, want 1 and %q", got.Code, got.Stderr, want)
	}
}
