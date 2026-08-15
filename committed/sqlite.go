package committed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLite(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

// VacuumInto writes a consistent snapshot of the database to destPath.
// Fails if destPath already exists.
func (s *SQLiteStore) VacuumInto(ctx context.Context, destPath string) error {
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, destPath)
	return err
}

// ── Pipelines ──

func (s *SQLiteStore) CreatePipeline(ctx context.Context, p *Pipeline) (int64, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pipelines (name, input_dir, output_dir, path_pattern, archive_dir, enable_merge, download_provider, scrape_providers, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.InputDir, p.OutputDir, p.PathPattern, p.ArchiveDir, p.EnableMerge, p.DownloadProvider, p.ScrapeProviders, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) DeletePipeline(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pipelines WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) GetPipeline(ctx context.Context, id int64) (*Pipeline, error) {
	p := &Pipeline{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, input_dir, output_dir, path_pattern, archive_dir, enable_merge, download_provider, scrape_providers, created_at, updated_at
		 FROM pipelines WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.InputDir, &p.OutputDir, &p.PathPattern, &p.ArchiveDir, &p.EnableMerge, &p.DownloadProvider, &p.ScrapeProviders, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *SQLiteStore) ListPipelines(ctx context.Context) ([]Pipeline, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, input_dir, output_dir, path_pattern, archive_dir, enable_merge, download_provider, scrape_providers, created_at, updated_at
		 FROM pipelines ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pipeline
	for rows.Next() {
		var p Pipeline
		if err := rows.Scan(&p.ID, &p.Name, &p.InputDir, &p.OutputDir, &p.PathPattern, &p.ArchiveDir, &p.EnableMerge, &p.DownloadProvider, &p.ScrapeProviders, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ── Provider Configs ──

func (s *SQLiteStore) GetProviderConfig(ctx context.Context, provider string) (string, error) {
	var config string
	err := s.db.QueryRowContext(ctx, `SELECT config FROM provider_configs WHERE provider = ?`, provider).Scan(&config)
	if errors.Is(err, sql.ErrNoRows) {
		return "{}", nil
	}
	return config, err
}

func (s *SQLiteStore) SetProviderConfig(ctx context.Context, provider, configJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO provider_configs (provider, config) VALUES (?, ?)
		 ON CONFLICT(provider) DO UPDATE SET config = excluded.config`,
		provider, configJSON,
	)
	return err
}

func (s *SQLiteStore) ListProviderConfigs(ctx context.Context) ([]ProviderConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider, config FROM provider_configs ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderConfig
	for rows.Next() {
		var pc ProviderConfig
		if err := rows.Scan(&pc.Provider, &pc.Config); err != nil {
			return nil, err
		}
		out = append(out, pc)
	}
	return out, rows.Err()
}

// ── Bangous ──

func (s *SQLiteStore) CreateBangou(ctx context.Context, b *Bangou) (int64, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO bangous (pipeline_id, number, out_dir, nfo_path, cover_path, raw_path, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.PipelineID, b.Number, b.OutDir, b.NFOPath, b.CoverPath, b.RawPath, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) GetBangou(ctx context.Context, id int64) (*Bangou, error) {
	b := &Bangou{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, pipeline_id, number, out_dir, nfo_path, cover_path, raw_path, created_at, updated_at
		 FROM bangous WHERE id = ?`, id,
	).Scan(&b.ID, &b.PipelineID, &b.Number, &b.OutDir, &b.NFOPath, &b.CoverPath, &b.RawPath, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *SQLiteStore) GetBangouByPipelineAndNumber(ctx context.Context, pipelineID int64, number string) (*Bangou, error) {
	b := &Bangou{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, pipeline_id, number, out_dir, nfo_path, cover_path, raw_path, created_at, updated_at
		 FROM bangous WHERE pipeline_id = ? AND number = ?`, pipelineID, number,
	).Scan(&b.ID, &b.PipelineID, &b.Number, &b.OutDir, &b.NFOPath, &b.CoverPath, &b.RawPath, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *SQLiteStore) UpdateBangouPaths(ctx context.Context, id int64, nfoPath, coverPath, rawPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE bangous SET nfo_path = ?, cover_path = ?, raw_path = ?, updated_at = ? WHERE id = ?`,
		nfoPath, coverPath, rawPath, time.Now(), id,
	)
	return err
}

func (s *SQLiteStore) DeleteBangou(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM metadata WHERE bangou_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM bangou_files WHERE bangou_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM bangous WHERE id = ?`, id)
	return err
}

func resolveBangouOrder(sort, order string) string {
	dir := "DESC"
	if order == "asc" {
		dir = "ASC"
	}
	switch sort {
	case "number":
		return "b.number " + dir
	case "date":
		return "COALESCE(m.premiered, m.year, '') " + dir + ", b.created_at DESC"
	case "rating":
		return "CAST(COALESCE(NULLIF(m.rating,''),'0') AS REAL) " + dir + ", b.created_at DESC"
	default: // "added"
		return "b.created_at " + dir
	}
}

func resolveBangouStatusWhere(status string) string {
	switch status {
	case "missing":
		return ` AND EXISTS (SELECT 1 FROM bangou_files bf WHERE bf.bangou_id = b.id AND bf.alive = FALSE)`
	case "alive":
		return ` AND EXISTS (SELECT 1 FROM bangou_files bf WHERE bf.bangou_id = b.id)
		         AND NOT EXISTS (SELECT 1 FROM bangou_files bf WHERE bf.bangou_id = b.id AND bf.alive = FALSE)`
	default:
		return ""
	}
}

// resolveBangouQWhere builds a case-insensitive LIKE filter on number and
// metadata title. Returns the SQL fragment (with leading " AND ") and the
// bound argument (or nil when q is empty).
func resolveBangouQWhere(q string) (string, any) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", nil
	}
	like := "%" + q + "%"
	return ` AND (b.number LIKE ? COLLATE NOCASE OR COALESCE(m.title,'') LIKE ? COLLATE NOCASE)`, like
}

func (s *SQLiteStore) ListBangousByPipeline(ctx context.Context, pipelineID int64, limit, offset int, sort, order, status, q string) ([]Bangou, int, error) {
	statusWhere := resolveBangouStatusWhere(status)
	qWhere, qArg := resolveBangouQWhere(q)
	where := statusWhere + qWhere

	var total int
	countQuery := `SELECT COUNT(*) FROM bangous b WHERE b.pipeline_id = ?` + where
	countArgs := []any{pipelineID}
	if qArg != nil {
		countArgs = append(countArgs, qArg, qArg)
	}
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		return nil, total, nil
	}

	orderClause := resolveBangouOrder(sort, order)
	query := fmt.Sprintf(
		`SELECT b.id, b.pipeline_id, b.number, b.out_dir, b.nfo_path, b.cover_path, b.raw_path, b.created_at, b.updated_at
		 FROM bangous b
		 LEFT JOIN metadata m ON b.id = m.bangou_id
		 WHERE b.pipeline_id = ?%s
		 ORDER BY %s
		 LIMIT ? OFFSET ?`, where, orderClause)

	listArgs := []any{pipelineID}
	if qArg != nil {
		listArgs = append(listArgs, qArg, qArg)
	}
	listArgs = append(listArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Bangou
	for rows.Next() {
		var b Bangou
		if err := rows.Scan(&b.ID, &b.PipelineID, &b.Number, &b.OutDir, &b.NFOPath, &b.CoverPath, &b.RawPath, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, b)
	}
	return out, total, rows.Err()
}

func (s *SQLiteStore) ListAllBangous(ctx context.Context) ([]Bangou, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, pipeline_id, number, out_dir, nfo_path, cover_path, raw_path, created_at, updated_at
		 FROM bangous ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bangou
	for rows.Next() {
		var b Bangou
		if err := rows.Scan(&b.ID, &b.PipelineID, &b.Number, &b.OutDir, &b.NFOPath, &b.CoverPath, &b.RawPath, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) IsBangouCommitted(ctx context.Context, pipelineID int64, number string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM bangous WHERE pipeline_id = ? AND number = ?`,
		pipelineID, number,
	).Scan(&count)
	return count > 0, err
}

// ── Bangou Files ──

func (s *SQLiteStore) CreateBangouFile(ctx context.Context, f *BangouFile) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bangou_files (bangou_id, src_path, link_path, link_type, file_size,
		                           resolution, video_codec, audio_codec, duration, bitrate,
		                           alive, created_at, checked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?, ?)`,
		f.BangouID, f.SrcPath, f.LinkPath, f.LinkType, f.FileSize,
		f.Resolution, f.VideoCodec, f.AudioCodec, f.Duration, f.Bitrate,
		time.Now(), time.Now(),
	)
	return err
}

const bangouFileCols = `id, bangou_id, src_path, link_path, link_type, file_size, resolution, video_codec, audio_codec, duration, bitrate, alive, created_at, checked_at`

func scanBangouFile(rows *sql.Rows) (BangouFile, error) {
	var f BangouFile
	err := rows.Scan(&f.ID, &f.BangouID, &f.SrcPath, &f.LinkPath, &f.LinkType,
		&f.FileSize, &f.Resolution, &f.VideoCodec, &f.AudioCodec, &f.Duration, &f.Bitrate,
		&f.Alive, &f.CreatedAt, &f.CheckedAt)
	return f, err
}

func (s *SQLiteStore) GetBangouFileByID(ctx context.Context, id int64) (*BangouFile, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM bangou_files WHERE id = ?`, bangouFileCols), id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	f, err := scanBangouFile(rows)
	if err != nil {
		return nil, err
	}
	return &f, rows.Err()
}

func (s *SQLiteStore) DeleteBangouFile(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bangou_files WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) ListBangouFilesByBangou(ctx context.Context, bangouID int64) ([]BangouFile, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM bangou_files WHERE bangou_id = ? ORDER BY created_at ASC`, bangouFileCols), bangouID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BangouFile
	for rows.Next() {
		f, err := scanBangouFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListAllBangouFiles(ctx context.Context) ([]BangouFile, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM bangou_files ORDER BY created_at DESC`, bangouFileCols),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BangouFile
	for rows.Next() {
		f, err := scanBangouFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SetBangouFileAlive(ctx context.Context, id int64, alive bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE bangou_files SET alive = ?, checked_at = ? WHERE id = ?`,
		alive, time.Now(), id,
	)
	return err
}

func (s *SQLiteStore) SetBangouFileLinkType(ctx context.Context, id int64, linkType string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE bangou_files SET link_type = ? WHERE id = ?`, linkType, id)
	return err
}

func (s *SQLiteStore) SetBangouFileSrcPath(ctx context.Context, id int64, srcPath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE bangou_files SET src_path = ? WHERE id = ?`, srcPath, id)
	return err
}

func (s *SQLiteStore) SetBangouFileMedia(ctx context.Context, id int64, fileSize int64, resolution, videoCodec, audioCodec, duration, bitrate string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE bangou_files SET file_size=?, resolution=?, video_codec=?, audio_codec=?, duration=?, bitrate=? WHERE id=?`,
		fileSize, resolution, videoCodec, audioCodec, duration, bitrate, id)
	return err
}

func (s *SQLiteStore) ListOrphanedBangouFiles(ctx context.Context) ([]BangouFile, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM bangou_files WHERE alive = FALSE ORDER BY checked_at DESC`, bangouFileCols),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BangouFile
	for rows.Next() {
		f, err := scanBangouFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CountBangouFiles(ctx context.Context, bangouID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bangou_files WHERE bangou_id = ?`, bangouID).Scan(&count)
	return count, err
}

// ── Metadata ──

func (s *SQLiteStore) UpsertMetadata(ctx context.Context, m *Metadata) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO metadata (bangou_id, number, title, plot, director, maker, label, series,
		                       actors, genres, cover_url, sample_images, premiered, year, runtime,
		                       rating, review_count, sample_movie_url, page_url, content_id, provider, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(bangou_id) DO UPDATE SET
		   number=excluded.number,
		   title=excluded.title, plot=excluded.plot, director=excluded.director,
		   maker=excluded.maker, label=excluded.label, series=excluded.series,
		   actors=excluded.actors, genres=excluded.genres, cover_url=excluded.cover_url,
		   sample_images=excluded.sample_images, premiered=excluded.premiered,
		   year=excluded.year, runtime=excluded.runtime, rating=excluded.rating,
		   review_count=excluded.review_count, sample_movie_url=excluded.sample_movie_url,
		   page_url=excluded.page_url,
		   content_id=excluded.content_id, provider=excluded.provider,
		   updated_at=excluded.updated_at`,
		m.BangouID, m.Number, m.Title, m.Plot, m.Director, m.Maker, m.Label, m.Series,
		m.Actors, m.Genres, m.CoverURL, m.SampleImages, m.Premiered, m.Year, m.Runtime,
		m.Rating, m.ReviewCount, m.SampleMovieURL, m.PageURL, m.ContentID, m.Provider, time.Now(),
	)
	return err
}

func (s *SQLiteStore) GetMetadataByBangou(ctx context.Context, bangouID int64) (*Metadata, error) {
	m := &Metadata{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, bangou_id, number, title, plot, director, maker, label, series,
		        actors, genres, cover_url, sample_images, premiered, year, runtime,
		        rating, review_count, sample_movie_url, page_url, content_id, provider,
		        created_at, updated_at
		 FROM metadata WHERE bangou_id = ?`, bangouID,
	).Scan(&m.ID, &m.BangouID, &m.Number, &m.Title, &m.Plot, &m.Director, &m.Maker, &m.Label, &m.Series,
		&m.Actors, &m.Genres, &m.CoverURL, &m.SampleImages, &m.Premiered, &m.Year, &m.Runtime,
		&m.Rating, &m.ReviewCount, &m.SampleMovieURL, &m.PageURL, &m.ContentID, &m.Provider,
		&m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ── Merged Parts ──

func (s *SQLiteStore) RecordMergedParts(ctx context.Context, parts []MergedPart) error {
	if len(parts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO merged_parts (number, filename, size, part, merged_at) VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	now := time.Now()
	for _, p := range parts {
		if _, err := stmt.ExecContext(ctx, p.Number, p.Filename, p.Size, p.Part, now); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetMergedParts(ctx context.Context, number string) ([]MergedPart, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT number, filename, size, part FROM merged_parts WHERE number = ? ORDER BY part ASC, id ASC`, number,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MergedPart
	for rows.Next() {
		var p MergedPart
		if err := rows.Scan(&p.Number, &p.Filename, &p.Size, &p.Part); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ── Legacy Settings ──

func (s *SQLiteStore) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *SQLiteStore) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (s *SQLiteStore) GetAllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
