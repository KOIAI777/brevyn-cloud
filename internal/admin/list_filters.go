package admin

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func parseListPagination(c *gin.Context, defaultLimit, maxLimit int) (int, int) {
	limit := parseBoundedInt(c.Query("limit"), defaultLimit, 1, maxLimit)
	offset := parseBoundedInt(c.Query("offset"), 0, 0, 1_000_000)
	return limit, offset
}

func appendDateRangeFilter(c *gin.Context, column string, where *[]string, args *[]any) error {
	return appendDateRangeValues(column, c.Query("dateFrom"), c.Query("dateTo"), where, args)
}

func appendDateRangeValues(column, dateFrom, dateTo string, where *[]string, args *[]any) error {
	if from, ok, err := parseAdminDateParam(dateFrom, false); err != nil {
		return err
	} else if ok {
		*args = append(*args, from)
		*where = append(*where, fmt.Sprintf("%s >= $%d", column, len(*args)))
	}
	if to, ok, err := parseAdminDateParam(dateTo, true); err != nil {
		return err
	} else if ok {
		*args = append(*args, to)
		*where = append(*where, fmt.Sprintf("%s < $%d", column, len(*args)))
	}
	return nil
}

func parseAdminDateParam(raw string, endExclusive bool) (time.Time, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false, nil
	}
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		return value, true, nil
	}
	value, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid_date")
	}
	if endExclusive {
		value = value.AddDate(0, 0, 1)
	}
	return value, true, nil
}

func appendExactStringFilter(where *[]string, args *[]any, column, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return
	}
	*args = append(*args, value)
	*where = append(*where, fmt.Sprintf("%s = $%d", column, len(*args)))
}
