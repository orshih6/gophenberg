// SPDX-License-Identifier: Apache-2.0

package app

import (
	"strings"
	"testing"

	"github.com/gopherium/framework/goncierge"
	"github.com/gopherium/framework/gonsole"
	"github.com/gopherium/framework/gonsole/testkit"
)

// sqliteAddress is a SQLite database address at an absolute path that no test opens.
const sqliteAddress = "sqlite:/srv/site/site.db"

// badSQLiteSettings holds, for each SQLite setting check reads, one value a SQLite site refuses.
var badSQLiteSettings = map[string]string{
	"GOPHENBERG_SQLITE_SYNCHRONOUS":       "off",
	"GOPHENBERG_DATABASE_MAX_CONNS":       "1",
	"GOPHENBERG_SQLITE_MIGRATE_LOCK_WAIT": "0",
}

func TestCheckNamesEachBadSQLiteSettingBeforeOpeningTheDatabase(t *testing.T) {
	t.Parallel()

	for name, value := range badSQLiteSettings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := testkit.Getenv(map[string]string{"GOPHENBERG_DATABASE_URL": sqliteAddress, name: value})

			got := testkit.Run(t, Program(env, noPlugins), "", "check")

			if want := "gophenberg: " + name + ": "; got.Code != gonsole.ExitFailed ||
				!strings.HasPrefix(got.Stderr, want) {
				t.Errorf("check = %d with stderr %q, want 1 and a line starting %q", got.Code, got.Stderr, want)
			}
		})
	}
}

func TestCheckNamesTheDatabaseAddressForABadSQLitePath(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"sqlite:site.db", "sqlite://x", "sqlite:/x?mode=ro"} {
		t.Run(address, func(t *testing.T) {
			t.Parallel()

			env := testkit.Getenv(map[string]string{"GOPHENBERG_DATABASE_URL": address})

			got := testkit.Run(t, Program(env, noPlugins), "", "check")

			if got.Code != gonsole.ExitFailed || !strings.Contains(got.Stderr, "GOPHENBERG_DATABASE_URL") {
				t.Errorf("check = %d with stderr %q, want 1 and GOPHENBERG_DATABASE_URL named", got.Code, got.Stderr)
			}
		})
	}
}

func TestCheckLeavesTheSQLiteSettingsAlongsideAPostgreSQLAddress(t *testing.T) {
	t.Parallel()

	for name, value := range badSQLiteSettings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := testkit.Getenv(map[string]string{"GOPHENBERG_DATABASE_URL": unreachableDatabaseURL, name: value})

			got := testkit.Run(t, Program(env, noPlugins), "", "check")

			if got.Code != gonsole.ExitDone || got.Stdout != "settings, plugins and command names are valid\n" {
				t.Errorf("check = %d, stdout %q, stderr %q, want 0 and every setting valid",
					got.Code, got.Stdout, got.Stderr)
			}
		})
	}
}

func TestCheckStillNamesABadCronLeaseOnASQLiteAddress(t *testing.T) {
	t.Parallel()

	env := testkit.Getenv(map[string]string{
		"GOPHENBERG_DATABASE_URL": sqliteAddress,
		"GOPHENBERG_CRON_LEASE":   "10s",
	})

	got := testkit.Run(t, Program(env, noPlugins), "", "check")

	if got.Code != gonsole.ExitFailed || !strings.Contains(got.Stderr, "GOPHENBERG_CRON_LEASE") {
		t.Errorf("check = %d with stderr %q, want 1 and GOPHENBERG_CRON_LEASE named", got.Code, got.Stderr)
	}
}

func TestCheckStillNamesAMalformedRecordSettingOnASQLiteAddress(t *testing.T) {
	t.Parallel()

	env := testkit.Getenv(map[string]string{
		"GOPHENBERG_DATABASE_URL":           sqlitePrefix + emptySQLiteFile(t),
		"GOPHENBERG_COMMAND_RECORD_TIMEOUT": "soon",
	})

	got := testkit.Run(t, Program(env, noPlugins), "", "check")

	if got.Code != gonsole.ExitFailed || !strings.Contains(got.Stderr, "GOPHENBERG_COMMAND_RECORD_TIMEOUT") {
		t.Errorf("check = %d with stderr %q, want 1 and GOPHENBERG_COMMAND_RECORD_TIMEOUT named",
			got.Code, got.Stderr)
	}
}

func TestCheckKeepsAPostgreSQLAddressThatMentionsTheSQLitePrefixOnPostgreSQL(t *testing.T) {
	t.Parallel()

	env := testkit.Getenv(map[string]string{
		"GOPHENBERG_DATABASE_URL": unreachableDatabaseURL + "&application_name=sqlite:importer",
	})

	got := testkit.Run(t, Program(env, noPlugins), "", "check")

	if got.Code != gonsole.ExitDone || got.Stdout != "settings, plugins and command names are valid\n" {
		t.Errorf("check = %d, stdout %q, stderr %q, want 0 and every setting valid", got.Code, got.Stdout, got.Stderr)
	}
}

func TestValidateAcceptsASQLiteAddressAtAnAbsolutePath(t *testing.T) {
	t.Parallel()

	call := gonsole.Call{Env: settingsEnv(testkit.Getenv(map[string]string{"GOPHENBERG_DATABASE_URL": sqliteAddress}))}

	if err := validate(accountConfig(goncierge.New(), call.Env.Getenv))(t.Context(), call); err != nil {
		t.Errorf("validate() error = %v, want nil", err)
	}
}
