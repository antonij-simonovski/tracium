package query

import (
	"context"
	"database/sql"

	"github.com/tracium/api/internal/model"
)

// bucketSeries runs a per-bucket aggregate query and materialises a gap-free,
// axis-aligned series over the window grid (bucketAxis): every slot is present;
// scanned buckets carry their value, absent buckets the zero value of V. scan
// reads one (bucket_ms, V) row; point builds an output element from a bucket
// timestamp and value, so each series owns its row shape. The query must emit
// bucket_ms on the same grid bucketAxis builds (see bucketMsExpr / intDiv).
func bucketSeries[V any, P any](
	ctx context.Context, db *sql.DB, f MetricsFilter, query string, args []any,
	scan func(*sql.Rows) (int64, V, error),
	point func(bucketMs int64, v V) P,
) ([]P, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byBucket := make(map[int64]V)
	for rows.Next() {
		bm, v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		byBucket[bm] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	base, bucketMs, n := bucketAxis(f)
	points := make([]P, n)
	for i := 0; i < n; i++ {
		bm := base + int64(i)*bucketMs
		points[i] = point(bm, byBucket[bm]) // absent bucket → zero value of V
	}
	return points, nil
}

// fillTrends runs a per-key, per-bucket query and writes each value into the
// matching ranked row's Trend slot on the shared bucket axis (zero-filled),
// mutating rows in place. keyOf returns a row's grouping key; alloc sizes its
// Trend to n slots; set assigns one slot; scan reads one (key, bucket_ms, value)
// row. Keys absent from rows (outside the top-N) are skipped. Empty rows is a no-op.
func fillTrends[T any, V any](
	ctx context.Context, db *sql.DB, f MetricsFilter, query string, args []any,
	rows []T,
	keyOf func(*T) string,
	alloc func(*T, int),
	set func(*T, int, V),
	scan func(*sql.Rows) (string, int64, V, error),
) error {
	if len(rows) == 0 {
		return nil
	}
	rs, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rs.Close()

	base, bucketMs, n := bucketAxis(f)
	idx := make(map[string]int, len(rows))
	for i := range rows {
		alloc(&rows[i], n)
		idx[keyOf(&rows[i])] = i
	}
	for rs.Next() {
		key, bm, v, err := scan(rs)
		if err != nil {
			return err
		}
		i, ok := idx[key]
		if !ok {
			continue
		}
		if slot := int((bm - base) / bucketMs); slot >= 0 && slot < n {
			set(&rows[i], slot, v)
		}
	}
	return rs.Err()
}

// fillWorkflowTrends fills each workflow row's per-bucket call-count sparkline
// from query; trend returns a row's name and its Trend field.
func fillWorkflowTrends[T any](ctx context.Context, db *sql.DB, f MetricsFilter, workflows []T, trend func(*T) (string, *[]int64), query string, args []any) error {
	return fillTrends(ctx, db, f, query, args, workflows,
		func(a *T) string { name, _ := trend(a); return name },
		func(a *T, n int) { _, t := trend(a); *t = make([]int64, n) },
		func(a *T, slot int, v int64) { _, t := trend(a); (*t)[slot] = v },
		scanNamedInt,
	)
}

func workflowCostTrend(a *model.WorkflowCost) (string, *[]int64) { return a.Name, &a.Trend }

func workflowTrend(a *model.Workflow) (string, *[]int64) { return a.Name, &a.Trend }

// zeroFillTrend expands a bucket_ms→count map (e.g. from a sumMap aggregate)
// onto the window's gap-free axis (bucketAxis): every slot is present and quiet
// buckets read 0, the shape the dashboard sparkline expects. Buckets outside the
// axis are ignored. Used by the single-scan ListWorkflows path.
func zeroFillTrend(f MetricsFilter, counts map[int64]int64) []int64 {
	base, bucketMs, n := bucketAxis(f)
	trend := make([]int64, n)
	for bm, c := range counts {
		if slot := int((bm - base) / bucketMs); slot >= 0 && slot < n {
			trend[slot] = c
		}
	}
	return trend
}

// fillUserTrends fills each user's per-bucket cost sparkline from query.
// Both the raw and rollup user-usage paths supply their own SQL and call this.
func (r *ClickHouseRepository) fillUserTrends(ctx context.Context, f MetricsFilter, users []model.UserUsage, query string, args []any) error {
	return fillTrends(ctx, r.db, f, query, args, users,
		func(t *model.UserUsage) string { return t.UserID },
		func(t *model.UserUsage, n int) { t.Trend = make([]float64, n) },
		func(t *model.UserUsage, slot int, v float64) { t.Trend[slot] = sanitize(v) },
		scanNamedFloat,
	)
}

func scanCostBucket(rows *sql.Rows) (int64, float64, error) {
	var bm int64
	var v float64
	err := rows.Scan(&bm, &v)
	return bm, sanitize(v), err
}

func costPoint(bm int64, v float64) model.CostPoint { return model.CostPoint{BucketMs: bm, Value: v} }

func scanErrorBucket(rows *sql.Rows) (int64, model.ErrorPoint, error) {
	var p model.ErrorPoint
	err := rows.Scan(&p.BucketMs, &p.Errors, &p.Total)
	return p.BucketMs, p, err
}

func errorPoint(bm int64, p model.ErrorPoint) model.ErrorPoint {
	p.BucketMs = bm // set the axis bucket so zero-filled (absent) slots are stamped too
	return p
}

func scanTokenBucket(rows *sql.Rows) (int64, model.TokenPoint, error) {
	var p model.TokenPoint
	err := rows.Scan(&p.BucketMs, &p.InputTokens, &p.OutputTokens)
	return p.BucketMs, p, err
}

func tokenPoint(bm int64, p model.TokenPoint) model.TokenPoint {
	p.BucketMs = bm
	return p
}

func scanNamedInt(rows *sql.Rows) (string, int64, int64, error) {
	var name string
	var bm, v int64
	err := rows.Scan(&name, &bm, &v)
	return name, bm, v, err
}

func scanNamedFloat(rows *sql.Rows) (string, int64, float64, error) {
	var name string
	var bm int64
	var v float64
	err := rows.Scan(&name, &bm, &v)
	return name, bm, v, err
}
