package monitoring

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/relay-ide/relay/engine/internal/broker"
)

// QueryableTables are the only tables user SQL may read (directly or through
// the v_insights_* views, which resolve to these). Connection profiles,
// preferences, and SQLite internals are deliberately excluded.
var QueryableTables = map[string]bool{
	"insights_systems":        true,
	"insights_epochs":         true,
	"insights_entities":       true,
	"insights_metrics":        true,
	"insights_findings":       true,
	"insights_finding_events": true,
}

// QueryableViews are advertised to the UI as the documented query surface.
var QueryableViews = []string{
	"v_insights_servers",
	"v_insights_connections",
	"v_insights_streams",
	"v_insights_consumers",
	"v_insights_findings_active",
}

const (
	MaxQueryLength     = 20_000
	DefaultQueryLimit  = 500
	MaxQueryLimit      = 5_000
	DefaultQueryTimout = 5 * time.Second
	MaxQueryTimeout    = 30 * time.Second
)

type QueryResult struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	RowCount   int      `json:"rowCount"`
	Truncated  bool     `json:"truncated"`
	Limit      int      `json:"limit"`
	DurationMs int64    `json:"durationMs"`
}

type SchemaObject struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Columns []string `json:"columns"`
}

// QuerySchema describes the objects user SQL may reference.
func QuerySchema(ctx context.Context, db *sql.DB) ([]SchemaObject, error) {
	names := append([]string{}, QueryableViews...)
	tables := make([]string, 0, len(QueryableTables))
	for t := range QueryableTables {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	names = append(names, tables...)
	out := make([]SchemaObject, 0, len(names))
	for i, n := range names {
		kind := "table"
		if i < len(QueryableViews) {
			kind = "view"
		}
		rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, n)
		if err != nil {
			return nil, err
		}
		obj := SchemaObject{Name: n, Kind: kind, Columns: []string{}}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err == nil {
				obj.Columns = append(obj.Columns, c)
			}
		}
		rows.Close()
		if len(obj.Columns) > 0 {
			out = append(out, obj)
		}
	}
	return out, nil
}

// Identifiers that reach SQLite internals or table-valued functions that are
// not covered by the bytecode table check (virtual tables have no rootpage).
var deniedIdentifier = regexp.MustCompile(`(?i)\b(sqlite_\w*|pragma_\w*|dbstat|load_extension|readfile|writefile|fts\d\w*)\b`)

var leadingKeyword = regexp.MustCompile(`(?is)^\s*(select|with)\b`)

// RunReadOnlyQuery executes a single SELECT/WITH statement against the
// read-only pool after verifying, from SQLite's compiled program, that it only
// reads allowlisted tables. ":system_id" is bound when present.
func RunReadOnlyQuery(ctx context.Context, db *sql.DB, systemID, sqlText string, limit int, timeout time.Duration) (*QueryResult, error) {
	q, err := normalizeQuery(sqlText)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultQueryLimit
	}
	if limit > MaxQueryLimit {
		limit = MaxQueryLimit
	}
	if timeout <= 0 {
		timeout = DefaultQueryTimout
	}
	if timeout > MaxQueryTimeout {
		timeout = MaxQueryTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var args []any
	if strings.Contains(q, ":system_id") {
		args = append(args, sql.Named("system_id", systemID))
	}

	if err := authorize(ctx, db, q, args); err != nil {
		return nil, err
	}

	wrapped := fmt.Sprintf("SELECT * FROM (\n%s\n) LIMIT %d", q, limit+1)
	start := time.Now()
	rows, err := db.QueryContext(ctx, wrapped, args...)
	if err != nil {
		return nil, queryError(ctx, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, queryError(ctx, err)
	}
	res := &QueryResult{Columns: cols, Rows: [][]any{}, Limit: limit}
	for rows.Next() {
		if len(res.Rows) >= limit {
			res.Truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, queryError(ctx, err)
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		res.Rows = append(res.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, queryError(ctx, err)
	}
	res.RowCount = len(res.Rows)
	res.DurationMs = time.Since(start).Milliseconds()
	return res, nil
}

func normalizeQuery(sqlText string) (string, error) {
	q := strings.TrimSpace(sqlText)
	if q == "" {
		return "", broker.NewError(broker.ErrValidation, "query is required", false)
	}
	if len(q) > MaxQueryLength {
		return "", broker.NewError(broker.ErrValidation, fmt.Sprintf("query exceeds %d characters", MaxQueryLength), false)
	}
	q = strings.TrimRight(q, "; \t\r\n")
	if strings.Contains(q, ";") {
		return "", broker.NewError(broker.ErrValidation, "only a single statement is allowed (remove ';')", false)
	}
	if !leadingKeyword.MatchString(q) {
		return "", broker.NewError(broker.ErrAuthorization, "only SELECT or WITH queries are allowed", false)
	}
	if m := deniedIdentifier.FindString(q); m != "" {
		return "", broker.NewError(broker.ErrAuthorization, fmt.Sprintf("%q is not available in the query explorer", m), false)
	}
	return q, nil
}

// authorize inspects the VDBE program for the query. Every btree the program
// opens must belong to an allowlisted table (or one of its indexes) in the
// main schema, and the program must not open anything for writing.
func authorize(ctx context.Context, db *sql.DB, q string, args []any) error {
	roots, err := rootpageOwners(ctx, db)
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "EXPLAIN "+q, args...)
	if err != nil {
		return queryError(ctx, err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(c)] = i
	}
	opI, p2I, p3I := idx["opcode"], idx["p2"], idx["p3"]
	denied := map[string]bool{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return queryError(ctx, err)
		}
		op := fmt.Sprint(asString(vals[opI]))
		switch op {
		case "OpenWrite", "Destroy", "Clear", "CreateBtree", "ParseSchema", "VUpdate", "VDestroy", "VCreate":
			return broker.NewError(broker.ErrAuthorization, "query attempts to modify data", false)
		case "OpenRead", "ReopenIdx":
			if asInt(vals[p3I]) != 0 {
				return broker.NewError(broker.ErrAuthorization, "only the main schema can be queried", false)
			}
			owner, ok := roots[asInt(vals[p2I])]
			if !ok || !QueryableTables[owner] {
				if owner == "" {
					owner = "an internal table"
				}
				denied[owner] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return queryError(ctx, err)
	}
	if len(denied) > 0 {
		names := make([]string, 0, len(denied))
		for n := range denied {
			names = append(names, n)
		}
		sort.Strings(names)
		return broker.NewError(broker.ErrAuthorization,
			fmt.Sprintf("query reads %s, which is outside the Insights query surface", strings.Join(names, ", ")), false)
	}
	return nil
}

// rootpageOwners maps btree root pages to the table that owns them (indexes
// map to their table). Root page 1 is sqlite_schema itself and is never owned
// by an allowlisted table.
func rootpageOwners(ctx context.Context, db *sql.DB) (map[int64]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT tbl_name, rootpage FROM sqlite_schema WHERE rootpage > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{1: "sqlite_schema"}
	for rows.Next() {
		var name string
		var root int64
		if err := rows.Scan(&name, &root); err != nil {
			return nil, err
		}
		out[root] = name
	}
	return out, rows.Err()
}

func queryError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return broker.NewError(broker.ErrTimeout, "query exceeded its time limit and was interrupted", true)
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return broker.NewError(broker.ErrCancelled, "query cancelled", false)
	}
	msg := strings.TrimPrefix(err.Error(), "SQL logic error: ")
	return broker.NewError(broker.ErrValidation, "query failed: "+msg, false)
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

func asInt(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case []byte:
		var n int64
		_, _ = fmt.Sscan(string(t), &n)
		return n
	case string:
		var n int64
		_, _ = fmt.Sscan(t, &n)
		return n
	}
	return -1
}
