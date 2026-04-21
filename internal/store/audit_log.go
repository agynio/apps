package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	auditLogEntryColumns      = `id, installation_id, message, level, idempotency_key, created_at`
	auditLogRetentionLimit    = 1000
	auditLogIdempotencyWindow = 24 * time.Hour
)

type InstallationAuditLogLevel string

const (
	InstallationAuditLogLevelInfo    InstallationAuditLogLevel = "info"
	InstallationAuditLogLevelWarning InstallationAuditLogLevel = "warning"
	InstallationAuditLogLevelError   InstallationAuditLogLevel = "error"
)

type InstallationAuditLogEntry struct {
	ID             uuid.UUID
	InstallationID uuid.UUID
	Message        string
	Level          InstallationAuditLogLevel
	IdempotencyKey *string
	CreatedAt      time.Time
}

type AppendInstallationAuditLogEntryInput struct {
	InstallationID uuid.UUID
	Message        string
	Level          InstallationAuditLogLevel
	IdempotencyKey *string
}

type auditLogCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func scanInstallationAuditLogEntry(row pgx.Row) (InstallationAuditLogEntry, error) {
	var entry InstallationAuditLogEntry
	var idempotencyKey pgtype.Text
	if err := row.Scan(
		&entry.ID,
		&entry.InstallationID,
		&entry.Message,
		&entry.Level,
		&idempotencyKey,
		&entry.CreatedAt,
	); err != nil {
		return InstallationAuditLogEntry{}, err
	}
	if idempotencyKey.Valid {
		entry.IdempotencyKey = &idempotencyKey.String
	}
	return entry, nil
}

func (s *Store) AppendInstallationAuditLogEntry(ctx context.Context, input AppendInstallationAuditLogEntryInput) (InstallationAuditLogEntry, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return InstallationAuditLogEntry{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", input.InstallationID.String()); err != nil {
		return InstallationAuditLogEntry{}, err
	}

	if input.IdempotencyKey != nil {
		cutoff := time.Now().Add(-auditLogIdempotencyWindow)
		row := tx.QueryRow(ctx,
			fmt.Sprintf(`SELECT %s FROM installation_audit_log_entries WHERE installation_id = $1 AND idempotency_key = $2 AND created_at >= $3 ORDER BY created_at DESC, id DESC LIMIT 1`, auditLogEntryColumns),
			input.InstallationID,
			*input.IdempotencyKey,
			cutoff,
		)
		entry, err := scanInstallationAuditLogEntry(row)
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return InstallationAuditLogEntry{}, err
			}
			return entry, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return InstallationAuditLogEntry{}, err
		}
	}

	row := tx.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO installation_audit_log_entries (installation_id, message, level, idempotency_key) VALUES ($1, $2, $3, $4) RETURNING %s`, auditLogEntryColumns),
		input.InstallationID,
		input.Message,
		input.Level,
		input.IdempotencyKey,
	)
	entry, err := scanInstallationAuditLogEntry(row)
	if err != nil {
		return InstallationAuditLogEntry{}, err
	}

	if _, err := tx.Exec(ctx,
		`WITH doomed AS (
            SELECT id FROM installation_audit_log_entries
            WHERE installation_id = $1
            ORDER BY created_at DESC, id DESC
            OFFSET $2
        )
        DELETE FROM installation_audit_log_entries WHERE id IN (SELECT id FROM doomed)`,
		input.InstallationID,
		auditLogRetentionLimit,
	); err != nil {
		return InstallationAuditLogEntry{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return InstallationAuditLogEntry{}, err
	}
	return entry, nil
}

func (s *Store) ListInstallationAuditLogEntries(ctx context.Context, installationID uuid.UUID, pageSize int, pageToken string) ([]InstallationAuditLogEntry, string, error) {
	limit := normalizePageSize(pageSize)

	args := []any{installationID}
	conditions := []string{"installation_id = $1"}
	argID := 1

	if pageToken != "" {
		cursor, err := decodeAuditLogPageToken(pageToken)
		if err != nil {
			return nil, "", InvalidPageToken(err)
		}
		args = append(args, cursor.CreatedAt, cursor.ID)
		conditions = append(conditions, fmt.Sprintf("(created_at < $%d OR (created_at = $%d AND id < $%d))", argID+1, argID+1, argID+2))
		argID += 2
	}

	query := fmt.Sprintf(
		"SELECT %s FROM installation_audit_log_entries WHERE %s ORDER BY created_at DESC, id DESC LIMIT $%d",
		auditLogEntryColumns,
		strings.Join(conditions, " AND "),
		argID+1,
	)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	entries := make([]InstallationAuditLogEntry, 0, limit)
	var (
		lastEntry InstallationAuditLogEntry
		hasMore   bool
	)
	for rows.Next() {
		if len(entries) == limit {
			hasMore = true
			break
		}
		entry, err := scanInstallationAuditLogEntry(rows)
		if err != nil {
			return nil, "", err
		}
		entries = append(entries, entry)
		lastEntry = entry
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextToken := ""
	if hasMore {
		nextToken = encodeAuditLogPageToken(lastEntry)
	}
	return entries, nextToken, nil
}

func encodeAuditLogPageToken(entry InstallationAuditLogEntry) string {
	payload := fmt.Sprintf("%d|%s", entry.CreatedAt.UnixNano(), entry.ID.String())
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeAuditLogPageToken(token string) (auditLogCursor, error) {
	if token == "" {
		return auditLogCursor{}, errors.New("empty token")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return auditLogCursor{}, fmt.Errorf("decode token: %w", err)
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return auditLogCursor{}, errors.New("invalid token")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return auditLogCursor{}, fmt.Errorf("parse timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return auditLogCursor{}, fmt.Errorf("parse id: %w", err)
	}
	return auditLogCursor{CreatedAt: time.Unix(0, nanos), ID: id}, nil
}
