package repository

import (
	"database/sql"
	"errors"
	"time"
)

var ErrHeartbeatNotFound = errors.New("service heartbeat not found")

type ServiceHeartbeatRepository struct {
	db Querier
}

func NewServiceHeartbeatRepository(db *sql.DB) *ServiceHeartbeatRepository {
	return &ServiceHeartbeatRepository{db: db}
}

func (r *ServiceHeartbeatRepository) Beat(component string) error {
	_, err := r.db.Exec(`
		INSERT INTO service_heartbeats (component, last_seen_at)
		VALUES ($1, NOW())
		ON CONFLICT (component) DO UPDATE SET last_seen_at = NOW()
	`, component)
	return err
}

func (r *ServiceHeartbeatRepository) LastSeen(component string) (time.Time, error) {
	var lastSeen time.Time
	err := r.db.QueryRow(`
		SELECT last_seen_at
		FROM service_heartbeats
		WHERE component = $1
	`, component).Scan(&lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrHeartbeatNotFound
	}
	if err != nil {
		return time.Time{}, err
	}
	return lastSeen, nil
}
