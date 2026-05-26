package agentruntime

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const sqlTextTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func (d SQLDialect) TimeType() string {
	if d == SQLDialectPostgres {
		return "TIMESTAMPTZ"
	}
	return "TEXT"
}

func sqlTimeValue(t time.Time, dialect SQLDialect) any {
	t = t.UTC()
	if dialect == SQLDialectPostgres {
		return t
	}
	return t.Format(sqlTextTimeLayout)
}

func nullableSQLTimeValue(t *time.Time, dialect SQLDialect) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return sqlTimeValue(*t, dialect)
}

func parseSQLTime(value any) (time.Time, error) {
	if value == nil {
		return time.Time{}, nil
	}
	switch v := value.(type) {
	case time.Time:
		return v.UTC(), nil
	case int64:
		return time.UnixMilli(v).UTC(), nil
	case int:
		return time.UnixMilli(int64(v)).UTC(), nil
	case int32:
		return time.UnixMilli(int64(v)).UTC(), nil
	case float64:
		return time.UnixMilli(int64(v)).UTC(), nil
	case []byte:
		return parseSQLTimeString(string(v))
	case string:
		return parseSQLTimeString(v)
	default:
		return time.Time{}, fmt.Errorf("unsupported SQL time value %T", value)
	}
}

func parseNullableSQLTime(value any) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseSQLTime(value)
	if err != nil || parsed.IsZero() {
		return nil, err
	}
	return &parsed, nil
}

func parseSQLTimeString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if ms, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.UnixMilli(ms).UTC(), nil
	}
	if parsed, err := time.Parse(sqlTextTimeLayout, value); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid SQL time %q", value)
}

func quotePostgresIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
