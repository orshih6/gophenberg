// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package app

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gopherium/framework/gonsole/testkit"
)

// lockedStepLimit bounds one schema step that waits on a migration lock another holder keeps.
const lockedStepLimit = 5 * time.Second

// holdMigrationLock holds the migration lock file beside the SQLite file at path until t ends.
func holdMigrationLock(t *testing.T, path string) {
	t.Helper()
	lock, err := os.OpenFile(path+".migrate.lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("opening the migration lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("holding the migration lock: %v", err)
	}
}

func TestEverySQLiteStepGivesUpAfterTheLockWaitTheSettingNames(t *testing.T) {
	t.Parallel()

	for index, name := range []string{"accounts", "records", "core"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := migratedSQLiteFile(t)
			db := openSQLiteFile(t, path)
			holdMigrationLock(t, path)
			steps := migrations(testkit.Getenv(map[string]string{
				"GOPHENBERG_DATABASE_URL":             sqlitePrefix + path,
				"GOPHENBERG_SQLITE_MIGRATE_LOCK_WAIT": "50ms",
				"GOPHENBERG_SQLITE_MIGRATE_LOCK_POLL": "10ms",
			}))
			if steps[index].Name != name {
				t.Fatalf("step %d is %q, want %q", index, steps[index].Name, name)
			}
			ctx, cancel := context.WithTimeout(t.Context(), lockedStepLimit)
			defer cancel()

			err := steps[index].RunOn(ctx, db)

			if want := "past the lock wait of 50ms"; err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("step %s under a held lock = %v, want an error naming %q", name, err, want)
			}
		})
	}
}
