package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	appColumns = `id, slug, name, description, icon, identity_id, service_token_hash, ziti_identity_id, ziti_service_id, created_at, updated_at`

	defaultListPageSize = 50
	maxListPageSize     = 100
)

type EntityMeta struct {
	ID        uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type App struct {
	Meta             EntityMeta
	Slug             string
	Name             string
	Description      string
	Icon             string
	IdentityID       uuid.UUID
	ServiceTokenHash string
	ZitiIdentityID   string
	ZitiServiceID    string
}

type CreateAppInput struct {
	ID               uuid.UUID
	Slug             string
	Name             string
	Description      string
	Icon             string
	IdentityID       uuid.UUID
	ServiceTokenHash string
	ZitiIdentityID   string
	ZitiServiceID    string
}

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func scanApp(row pgx.Row) (App, error) {
	var app App
	if err := row.Scan(
		&app.Meta.ID,
		&app.Slug,
		&app.Name,
		&app.Description,
		&app.Icon,
		&app.IdentityID,
		&app.ServiceTokenHash,
		&app.ZitiIdentityID,
		&app.ZitiServiceID,
		&app.Meta.CreatedAt,
		&app.Meta.UpdatedAt,
	); err != nil {
		return App{}, err
	}
	return app, nil
}

func (s *Store) CreateApp(ctx context.Context, input CreateAppInput) (App, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO apps (id, slug, name, description, icon, identity_id, service_token_hash, ziti_identity_id, ziti_service_id)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
         RETURNING %s`, appColumns),
		input.ID,
		input.Slug,
		input.Name,
		input.Description,
		input.Icon,
		input.IdentityID,
		input.ServiceTokenHash,
		input.ZitiIdentityID,
		input.ZitiServiceID,
	)
	app, err := scanApp(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return App{}, AlreadyExists("app")
		}
		return App{}, err
	}
	return app, nil
}

func (s *Store) GetApp(ctx context.Context, id uuid.UUID) (App, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM apps WHERE id = $1`, appColumns),
		id,
	)
	app, err := scanApp(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return App{}, NotFound("app")
		}
		return App{}, err
	}
	return app, nil
}

func (s *Store) GetAppBySlug(ctx context.Context, slug string) (App, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM apps WHERE slug = $1`, appColumns),
		slug,
	)
	app, err := scanApp(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return App{}, NotFound("app")
		}
		return App{}, err
	}
	return app, nil
}

func (s *Store) GetAppByIdentityID(ctx context.Context, identityID uuid.UUID) (App, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM apps WHERE identity_id = $1`, appColumns),
		identityID,
	)
	app, err := scanApp(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return App{}, NotFound("app")
		}
		return App{}, err
	}
	return app, nil
}

func (s *Store) GetAppByServiceTokenHash(ctx context.Context, tokenHash string) (App, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM apps WHERE service_token_hash = $1`, appColumns),
		tokenHash,
	)
	app, err := scanApp(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return App{}, NotFound("app")
		}
		return App{}, err
	}
	return app, nil
}

func (s *Store) ListApps(ctx context.Context, pageSize int, pageToken string) ([]App, string, error) {
	limit := normalizePageSize(pageSize)

	var (
		clauses []string
		args    []any
	)
	if pageToken != "" {
		afterID, err := decodePageToken(pageToken)
		if err != nil {
			return nil, "", InvalidPageToken(err)
		}
		clauses = append(clauses, fmt.Sprintf("id > $%d", len(args)+1))
		args = append(args, afterID)
	}

	query := strings.Builder{}
	query.WriteString(fmt.Sprintf("SELECT %s FROM apps", appColumns))
	if len(clauses) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(clauses, " AND "))
	}
	query.WriteString(fmt.Sprintf(" ORDER BY id ASC LIMIT $%d", len(args)+1))
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	apps := make([]App, 0, limit)
	var (
		lastID  uuid.UUID
		hasMore bool
	)
	for rows.Next() {
		if len(apps) == limit {
			hasMore = true
			break
		}
		app, err := scanApp(rows)
		if err != nil {
			return nil, "", err
		}
		apps = append(apps, app)
		lastID = app.Meta.ID
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextToken := ""
	if hasMore {
		nextToken = encodePageToken(lastID)
	}
	return apps, nextToken, nil
}

func (s *Store) DeleteApp(ctx context.Context, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM apps WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return NotFound("app")
	}
	return nil
}

func normalizePageSize(size int) int {
	if size <= 0 {
		return defaultListPageSize
	}
	if size > maxListPageSize {
		return maxListPageSize
	}
	return size
}

func encodePageToken(id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String()))
}

func decodePageToken(token string) (uuid.UUID, error) {
	if token == "" {
		return uuid.UUID{}, errors.New("empty token")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("decode token: %w", err)
	}
	value, err := uuid.Parse(string(decoded))
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("parse token: %w", err)
	}
	return value, nil
}
