// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gopherium/framework/gonsole"
	"github.com/gopherium/framework/gonsole/testkit"
)

// binaryRunLimit is how long one run of the built binary may take before the test stops it.
const binaryRunLimit = 30 * time.Second

// runBinary runs the built binary with args under env from a folder of its own and returns what it answered.
func runBinary(t *testing.T, binary string, env []string, args ...string) testkit.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), binaryRunLimit)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = env
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	var exitErr *exec.ExitError
	if err := cmd.Run(); err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("gophenberg %q: %v", args, err)
	}
	return testkit.Result{Code: cmd.ProcessState.ExitCode(), Stdout: stdout.String(), Stderr: stderr.String()}
}

// cronJobCount returns how many cron jobs the SQLite file behind db holds.
func cronJobCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	lookup := "SELECT count(*) FROM core_cron_jobs"
	if err := db.QueryRowContext(t.Context(), lookup).Scan(&count); err != nil {
		t.Fatalf("%s: %v", lookup, err)
	}
	return count
}

func TestSQLiteBinaryMigratesChecksAndNamesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	binary, env := testkit.CoverBinary(t, "GOPHENBERG_", "gophenberg")
	path := emptySQLiteFile(t)
	env = append(env, "GOPHENBERG_DATABASE_URL="+sqlitePrefix+path, "GOPHENBERG_ADDR=localhost:0")

	migrated := runBinary(t, binary, env, "migrate")
	checked := runBinary(t, binary, env, "check")
	served := runBinary(t, binary, env, "serve")
	listed := runBinary(t, binary, env, "account:list")

	if migrated.Code != gonsole.ExitDone || migrated.Stdout != migratedLines {
		t.Errorf("gophenberg migrate = %d, stdout %q, stderr %q, want 0 and %q",
			migrated.Code, migrated.Stdout, migrated.Stderr, migratedLines)
	}
	db := openSQLiteFile(t, path)
	for _, table := range []string{
		"auth_goose_db_version", "gonsole_goose_db_version", "core_goose_db_version", "core_cron_jobs",
	} {
		if !hasTable(t, db, table) {
			t.Errorf("the file holds no %s table after gophenberg migrate, want every owner's schema", table)
		}
	}
	if want := "settings, plugins and command names are valid\n"; checked.Code != gonsole.ExitDone ||
		checked.Stdout != want {
		t.Errorf("gophenberg check = %d, stdout %q, stderr %q, want 0 and %q",
			checked.Code, checked.Stdout, checked.Stderr, want)
	}
	if want := "serve: " + errPostgresOnly.Error(); served.Code != gonsole.ExitFailed ||
		!strings.Contains(served.Stderr, want) {
		t.Errorf("gophenberg serve = %d with stderr %q, want 1 and %q", served.Code, served.Stderr, want)
	}
	if jobs := cronJobCount(t, db); jobs != 0 {
		t.Errorf("core_cron_jobs holds %d rows after gophenberg serve, want none since no cron job ran", jobs)
	}
	if want := "account commands: " + errPostgresOnly.Error(); listed.Code != gonsole.ExitFailed ||
		!strings.Contains(listed.Stderr, want) {
		t.Errorf("gophenberg account:list = %d with stderr %q, want 1 and %q", listed.Code, listed.Stderr, want)
	}
}
