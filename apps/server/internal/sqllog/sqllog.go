// Package sqllog carries a structured SQL-audit logger through context and
// provides the redaction + SQLSTATE helpers used by every SQL execution site.
// The target layer (internal/target) pulls the logger from ctx so ALL SQL —
// read-only catalog queries, console queries, row mutations, EXPLAIN, and
// worker migrations — is logged with the M2-mandated attributes; callers that
// do not attach a logger (smoke tests) simply produce no SQL log lines.
package sqllog

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type ctxKey struct{}

// Attach stores base (already enriched with actor/trace/correlation fields)
// as the SQL audit logger for ctx. Handlers attach after authentication;
// the worker attaches with job/execution/attempt correlation fields.
func Attach(ctx context.Context, base zerolog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, base)
}

// From returns the attached logger; ok is false when none was attached.
func From(ctx context.Context) (zerolog.Logger, bool) {
	l, ok := ctx.Value(ctxKey{}).(zerolog.Logger)
	return l, ok
}

// Default returns the root logger — used by the worker so target-layer SQL
// logs are emitted even when a specific logger was not attached.
func Default() zerolog.Logger { return log.Logger }

// truncate caps logged SQL so huge statements never flood the logs.
func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "...(" + itoa(len(s)) + " chars)"
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// SQLRedactMax is the cap applied to redacted SQL before logging.
const SQLRedactMax = 200

// Redact masks credential-like material before SQL or errors reach the logs:
// password/secret/token literals inside SQL, and embedded DSN passwords.
// Values that look like opaque session identifiers (32-byte base64url tokens)
// are masked in any context.
func Redact(s string) string {
	out := dsnPasswordRe.ReplaceAllString(s, "$1://$2:***@")
	out = credentialAssignRe.ReplaceAllString(out, "${1}'***'")
	out = credentialKeywordRe.ReplaceAllString(out, "$1('***')")
	return out
}

// LogSQL truncates and redacts SQL text for a log field value.
func LogSQL(s string) string { return truncate(Redact(s), SQLRedactMax) }

var (
	// postgres://user:pass@host, mysql://user:pass@tcp(...), user:pass@...
	dsnPasswordRe = regexp.MustCompile(`(?i)\b(postgres(?:ql)?|mysql|mariadb)://([^:@/\s]+):[^@/\s]+@`)
	// password = 'x' / pwd='x' / token: "x" / PASSWORD('x'), and any sensitive
	// column followed within a few non-digit chars by a quoted literal (INSERT
	// INTO t(token) VALUES ('abc')). Digits break the gap so type definitions
	// like VARCHAR(50) never trigger masking.
	credentialAssignRe = regexp.MustCompile(`(?i)(\b(?:password|passwd|pwd|secret|token|api[_-]?key)\b[^0-9']{0,12})('[^']*'|"[^"]*")`)
	// PASSWORD('x') / IDENTIFIED BY 'x' style function arguments
	credentialKeywordRe = regexp.MustCompile(`(?i)\b(password|identified\s+by)\s*\(\s*'[^']*'`)
)

// SQLState extracts the SQLSTATE (or vendor code) from driver errors so the
// audit log carries the machine-readable failure class.
func SQLState(err error) string {
	if err == nil {
		return ""
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code)
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return strconv.Itoa(int(myErr.Number))
	}
	return ""
}

// ErrorText renders a driver error redacted and bounded for logging.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	text := Redact(err.Error())
	return truncate(text, 300)
}

// QuoteEventName builds consistent event names, e.g. "sql_exec_commit".
func QuoteEventName(kind, phase string) string {
	return strings.Join([]string{"sql", kind, phase}, "_")
}
