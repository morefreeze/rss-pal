package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

type mediaRecordingDriver struct {
	conn *mediaRecordingConn
}

func (d *mediaRecordingDriver) Open(string) (driver.Conn, error) {
	return d.conn, nil
}

type mediaRecordingConn struct {
	query string
	args  []driver.NamedValue
}

func (c *mediaRecordingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare is not supported")
}
func (c *mediaRecordingConn) Close() error { return nil }
func (c *mediaRecordingConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Begin is not supported")
}
func (c *mediaRecordingConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.query = query
	c.args = append([]driver.NamedValue(nil), args...)
	return driver.RowsAffected(1), nil
}

func TestUpdateMediaResetsTranscriptEligibilityAndStoresNullsWhenClearing(t *testing.T) {
	recorder := &mediaRecordingConn{}
	sql.Register("rsspal-media-recording", &mediaRecordingDriver{conn: recorder})
	db, err := sql.Open("rsspal-media-recording", "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := NewArticleRepository(db).UpdateMedia(2692, "", "", 0); err != nil {
		t.Fatalf("UpdateMedia: %v", err)
	}
	if !strings.Contains(recorder.query, "transcript_fetched_at = NULL") {
		t.Fatalf("UpdateMedia query does not reset transcript eligibility:\n%s", recorder.query)
	}
	if len(recorder.args) != 4 {
		t.Fatalf("UpdateMedia args = %d, want 4", len(recorder.args))
	}
	for i, arg := range recorder.args[1:] {
		if arg.Value != nil {
			t.Fatalf("clear arg %d = %#v, want nil", i+2, arg.Value)
		}
	}
}
