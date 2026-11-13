package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountRepositoryClearObservedRateLimitOAuthGateAndPropagation(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale", true: "applied"}[applied], func(t *testing.T) {
			var query string
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				if expected == "capture recovery" {
					query = actual
					return nil
				}
				return sqlmock.QueryMatcherRegexp.Match(expected, actual)
			})))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			repo := newAccountRepositoryWithSQL(client, db, nil)
			limitedAt, resetAt := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
			rows := int64(0)
			if applied {
				rows = 1
			}
			mock.ExpectExec("capture recovery").
				WithArgs(sqlmock.AnyArg(), int64(42), service.PlatformGrok, service.PlatformOpenAI, service.AccountTypeOAuth, limitedAt, resetAt).
				WillReturnResult(sqlmock.NewResult(0, rows))
			if applied {
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
					WithArgs(service.SchedulerOutboxEventAccountChanged, int64(42), nil, nil, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))
			}
			cleared, err := repo.ClearRateLimitIfObserved(context.Background(), 42, limitedAt, resetAt)
			require.NoError(t, err)
			require.Equal(t, applied, cleared)
			query = strings.ReplaceAll(query, `"accounts".`, "")
			require.Regexp(t, `\("platform" = \$\d+ OR \("platform" = \$\d+ AND "parent_account_id" IS NULL\)\)`, query)
			for _, field := range []string{"type", "rate_limited_at", "rate_limit_reset_at"} {
				require.Regexp(t, `AND "`+field+`" = \$\d+`, query)
			}
			require.Contains(t, query, `"rate_limited_at" = NULL`)
			require.Contains(t, query, `"rate_limit_reset_at" = NULL`)
			require.NotContains(t, query, `"overload_until" =`)
			require.NotContains(t, query, `"temp_unschedulable_until" =`)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
