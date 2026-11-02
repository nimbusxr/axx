package sql

import (
	"context"
	"fmt"
	"time"
)

// databaseNow is the moment by the database's own clock, in its time zone: what a seed's
// relative times count from. A column with no time zone holds a wall clock, and what
// compares it (Oracle's SYSDATE, MySQL's NOW()) reads the database's, so the machine
// running axx, in its own zone and with its own idea of the time, has no say in it.
// SQLite has no clock or zone of its own, so it has the machine's.
func (svc *Service) databaseNow(ctx context.Context) (time.Time, error) {
	var q string
	switch d := svc.Target.Dialect; {
	case d.Oracle:
		q = "SELECT TO_CHAR(SYSTIMESTAMP, 'YYYY-MM-DD HH24:MI:SS.FF6'), " +
			"EXTRACT(TIMEZONE_HOUR FROM SYSTIMESTAMP) * 60 + EXTRACT(TIMEZONE_MINUTE FROM SYSTIMESTAMP) FROM dual"
	case d.Postgres:
		q = "SELECT to_char(now(), 'YYYY-MM-DD HH24:MI:SS.US'), (EXTRACT(TIMEZONE FROM now()) / 60)::int"
	case d.Name == mysql.Name:
		q = "SELECT DATE_FORMAT(NOW(6), '%Y-%m-%d %H:%i:%s.%f'), TIMESTAMPDIFF(MINUTE, UTC_TIMESTAMP(6), NOW(6))"
	case d.Name == sqlserver.Name:
		q = "SELECT CONVERT(varchar(26), SYSDATETIME(), 121), DATEPART(TZOFFSET, SYSDATETIMEOFFSET())"
	default:
		return time.Now(), nil
	}
	var wall string
	var minutes int
	if err := svc.DB.QueryRowContext(ctx, q).Scan(&wall, &minutes); err != nil {
		return time.Time{}, err
	}
	return parseDatabaseNow(wall, minutes)
}

// parseDatabaseNow is the wall clock a database gave, at its offset in minutes.
func parseDatabaseNow(wall string, minutes int) (time.Time, error) {
	at, err := time.ParseInLocation("2006-01-02 15:04:05", wall, time.FixedZone("", minutes*60))
	if err != nil {
		return time.Time{}, fmt.Errorf("its time reads as %q: %w", wall, err)
	}
	return at, nil
}
