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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	appColumns          = `id, slug, name, description, icon, identity_id, service_token_hash, ziti_identity_id, ziti_service_id, organization_id, visibility, permissions, created_at, updated_at`
	installationColumns = `id, app_id, organization_id, slug, configuration, status, created_at, updated_at`

	defaultListPageSize = 50
	maxListPageSize     = 100
)

type EntityMeta struct {
	ID        uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AppVisibility string

const (
	AppVisibilityPublic   AppVisibility = "public"
	AppVisibilityInternal AppVisibility = "internal"
)

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
	OrganizationID   uuid.UUID
	Visibility       AppVisibility
	Permissions      []string
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
	OrganizationID   uuid.UUID
	Visibility       AppVisibility
	Permissions      []string
}

type UpdateAppInput struct {
	ID          uuid.UUID
	Name        *string
	Description *string
	Icon        *string
	Visibility  *AppVisibility
}

type Installation struct {
	Meta           EntityMeta
	AppID          uuid.UUID
	OrganizationID uuid.UUID
	Slug           string
	Configuration  map[string]any
	Status         *string
}

type CreateInstallationInput struct {
	ID             uuid.UUID
	AppID          uuid.UUID
	OrganizationID uuid.UUID
	Slug           string
	Configuration  map[string]any
}

type UpdateInstallationInput struct {
	ID            uuid.UUID
	Slug          *string
	Configuration *map[string]any
}

type ListAppsFilter struct {
	OrganizationID *uuid.UUID
	Visibility     *AppVisibility
}

type ListInstallationsFilter struct {
	OrganizationID *uuid.UUID
	AppID          *uuid.UUID
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
		&app.OrganizationID,
		&app.Visibility,
		&app.Permissions,
		&app.Meta.CreatedAt,
		&app.Meta.UpdatedAt,
	); err != nil {
		return App{}, err
	}
	return app, nil
}

func scanInstallation(row pgx.Row) (Installation, error) {
	var installation Installation
	var status pgtype.Text
	if err := row.Scan(
		&installation.Meta.ID,
		&installation.AppID,
		&installation.OrganizationID,
		&installation.Slug,
		&installation.Configuration,
		&status,
		&installation.Meta.CreatedAt,
		&installation.Meta.UpdatedAt,
	); err != nil {
		return Installation{}, err
	}
	if status.Valid {
		installation.Status = &status.String
	}
	return installation, nil
}

func (s *Store) CreateApp(ctx context.Context, input CreateAppInput) (App, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO apps (id, slug, name, description, icon, identity_id, service_token_hash, ziti_identity_id, ziti_service_id, organization_id, visibility, permissions)
	         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
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
		input.OrganizationID,
		input.Visibility,
		input.Permissions,
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

func (s *Store) GetAppBySlug(ctx context.Context, organizationID uuid.UUID, slug string) (App, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM apps WHERE organization_id = $1 AND slug = $2`, appColumns),
		organizationID,
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

type listQueryBuilder struct {
	conditions []string
	args       []any
	argID      int
}

func newListQueryBuilder(pageToken string) (*listQueryBuilder, error) {
	builder := &listQueryBuilder{
		conditions: make([]string, 0, 3),
		args:       make([]any, 0, 4),
		argID:      1,
	}
	if pageToken == "" {
		return builder, nil
	}
	afterID, err := decodePageToken(pageToken)
	if err != nil {
		return nil, err
	}
	builder.addRawCondition("id > $%d", afterID)
	return builder, nil
}

func (b *listQueryBuilder) addCondition(column string, value any) {
	b.conditions = append(b.conditions, fmt.Sprintf("%s = $%d", column, b.argID))
	b.args = append(b.args, value)
	b.argID++
}

func (b *listQueryBuilder) addRawCondition(format string, value any) {
	b.conditions = append(b.conditions, fmt.Sprintf(format, b.argID))
	b.args = append(b.args, value)
	b.argID++
}

func (b *listQueryBuilder) build(baseQuery string, limit int) (string, []any) {
	query := baseQuery
	if len(b.conditions) > 0 {
		query = fmt.Sprintf("%s WHERE %s", query, strings.Join(b.conditions, " AND "))
	}
	query = fmt.Sprintf("%s ORDER BY id ASC LIMIT $%d", query, b.argID)
	args := append(b.args, limit+1)
	return query, args
}

func (s *Store) ListApps(ctx context.Context, pageSize int, pageToken string, filter ListAppsFilter) ([]App, string, error) {
	limit := normalizePageSize(pageSize)

	builder, err := newListQueryBuilder(pageToken)
	if err != nil {
		return nil, "", InvalidPageToken(err)
	}
	if filter.OrganizationID != nil {
		builder.addCondition("organization_id", *filter.OrganizationID)
	}
	if filter.Visibility != nil {
		builder.addCondition("visibility", *filter.Visibility)
	}
	query, args := builder.build(fmt.Sprintf("SELECT %s FROM apps", appColumns), limit)
	rows, err := s.pool.Query(ctx, query, args...)
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

func (s *Store) UpdateApp(ctx context.Context, input UpdateAppInput) (App, error) {
	setParts := make([]string, 0, 5)
	args := make([]any, 0, 6)
	argID := 1
	if input.Name != nil {
		setParts = append(setParts, fmt.Sprintf("name = $%d", argID))
		args = append(args, *input.Name)
		argID++
	}
	if input.Description != nil {
		setParts = append(setParts, fmt.Sprintf("description = $%d", argID))
		args = append(args, *input.Description)
		argID++
	}
	if input.Icon != nil {
		setParts = append(setParts, fmt.Sprintf("icon = $%d", argID))
		args = append(args, *input.Icon)
		argID++
	}
	if input.Visibility != nil {
		setParts = append(setParts, fmt.Sprintf("visibility = $%d", argID))
		args = append(args, *input.Visibility)
		argID++
	}
	setParts = append(setParts, "updated_at = NOW()")
	args = append(args, input.ID)
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf("UPDATE apps SET %s WHERE id = $%d RETURNING %s", strings.Join(setParts, ", "), argID, appColumns),
		args...,
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

func (s *Store) HasActiveInstallations(ctx context.Context, appID uuid.UUID) (bool, error) {
	row := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_installations WHERE app_id = $1)`, appID)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Store) UpdateAppZitiIdentity(ctx context.Context, id uuid.UUID, zitiIdentityID string, zitiServiceID string) error {
	result, err := s.pool.Exec(ctx,
		`UPDATE apps SET ziti_identity_id = $1, ziti_service_id = $2, updated_at = NOW() WHERE id = $3`,
		zitiIdentityID, zitiServiceID, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return NotFound("app")
	}
	return nil
}

func (s *Store) CreateInstallation(ctx context.Context, input CreateInstallationInput) (Installation, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO app_installations (id, app_id, organization_id, slug, configuration)
	         VALUES ($1, $2, $3, $4, $5)
	         RETURNING %s`, installationColumns),
		input.ID,
		input.AppID,
		input.OrganizationID,
		input.Slug,
		input.Configuration,
	)
	installation, err := scanInstallation(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Installation{}, AlreadyExists("installation")
		}
		return Installation{}, err
	}
	return installation, nil
}

func (s *Store) GetInstallation(ctx context.Context, id uuid.UUID) (Installation, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM app_installations WHERE id = $1`, installationColumns),
		id,
	)
	installation, err := scanInstallation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, NotFound("installation")
		}
		return Installation{}, err
	}
	return installation, nil
}

func (s *Store) GetInstallationBySlug(ctx context.Context, organizationID uuid.UUID, slug string) (Installation, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM app_installations WHERE organization_id = $1 AND slug = $2`, installationColumns),
		organizationID,
		slug,
	)
	installation, err := scanInstallation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, NotFound("installation")
		}
		return Installation{}, err
	}
	return installation, nil
}

func (s *Store) ListInstallations(ctx context.Context, pageSize int, pageToken string, filter ListInstallationsFilter) ([]Installation, string, error) {
	limit := normalizePageSize(pageSize)

	builder, err := newListQueryBuilder(pageToken)
	if err != nil {
		return nil, "", InvalidPageToken(err)
	}
	if filter.OrganizationID != nil {
		builder.addCondition("organization_id", *filter.OrganizationID)
	}
	if filter.AppID != nil {
		builder.addCondition("app_id", *filter.AppID)
	}
	query, args := builder.build(fmt.Sprintf("SELECT %s FROM app_installations", installationColumns), limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	installations := make([]Installation, 0, limit)
	var (
		lastID  uuid.UUID
		hasMore bool
	)
	for rows.Next() {
		if len(installations) == limit {
			hasMore = true
			break
		}
		installation, err := scanInstallation(rows)
		if err != nil {
			return nil, "", err
		}
		installations = append(installations, installation)
		lastID = installation.Meta.ID
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextToken := ""
	if hasMore {
		nextToken = encodePageToken(lastID)
	}
	return installations, nextToken, nil
}

func (s *Store) UpdateInstallation(ctx context.Context, input UpdateInstallationInput) (Installation, error) {
	setParts := make([]string, 0, 3)
	args := make([]any, 0, 4)
	argID := 1
	if input.Slug != nil {
		setParts = append(setParts, fmt.Sprintf("slug = $%d", argID))
		args = append(args, *input.Slug)
		argID++
	}
	if input.Configuration != nil {
		setParts = append(setParts, fmt.Sprintf("configuration = $%d", argID))
		args = append(args, *input.Configuration)
		argID++
	}
	setParts = append(setParts, "updated_at = NOW()")
	args = append(args, input.ID)
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf("UPDATE app_installations SET %s WHERE id = $%d RETURNING %s", strings.Join(setParts, ", "), argID, installationColumns),
		args...,
	)
	installation, err := scanInstallation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, NotFound("installation")
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Installation{}, AlreadyExists("installation")
		}
		return Installation{}, err
	}
	return installation, nil
}

func (s *Store) UpdateInstallationStatus(ctx context.Context, id uuid.UUID, status *string) (Installation, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf("UPDATE app_installations SET status = $1, updated_at = NOW() WHERE id = $2 RETURNING %s", installationColumns),
		status,
		id,
	)
	installation, err := scanInstallation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Installation{}, NotFound("installation")
		}
		return Installation{}, err
	}
	return installation, nil
}

func (s *Store) DeleteInstallation(ctx context.Context, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM app_installations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return NotFound("installation")
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
