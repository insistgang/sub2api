package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestBatchAPIKeyUsageStatsSeparatesStandardAndActualCosts(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	mock.ExpectQuery(`SELECT api_key_id, COALESCE\(SUM\(actual_cost\)`).
		WithArgs(sqlmock.AnyArg(), start, end, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"api_key_id", "total_actual_cost", "today_actual_cost", "total_cost", "today_cost",
		}).AddRow(7, 0.5, 0.05, 2.5, 0.25))

	stats, err := repo.GetBatchAPIKeyUsageStats(context.Background(), []int64{7, 8}, start, end)
	require.NoError(t, err)
	require.InDelta(t, 2.5, stats[7].TotalCost, 1e-9)
	require.InDelta(t, 0.25, stats[7].TodayCost, 1e-9)
	require.InDelta(t, 0.5, stats[7].TotalActualCost, 1e-9)
	require.InDelta(t, 0.05, stats[7].TodayActualCost, 1e-9)
	require.Zero(t, stats[8].TotalCost)
	require.Zero(t, stats[8].TodayCost)
	require.NoError(t, mock.ExpectationsWereMet())
}
