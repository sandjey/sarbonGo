package cargo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrPendingCargoPhotoNotFound is returned when a pending photo id is missing or already claimed.
var ErrPendingCargoPhotoNotFound = errors.New("pending cargo photo not found")

const pendingPhotoPathMarker = "/api/cargo/photos/"

func cargoStorageRoot() string {
	storageRoot := strings.TrimSpace(os.Getenv("CARGO_STORAGE_DIR"))
	if storageRoot == "" {
		storageRoot = "storage"
	}
	return storageRoot
}

// ParsePendingCargoPhotoRef returns (id, true) if s is a reference to a pre-uploaded pending photo:
// either a URL/path containing /api/cargo/photos/{uuid}, or a bare UUID string.
func ParsePendingCargoPhotoRef(s string) (uuid.UUID, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return uuid.Nil, false
	}
	if i := strings.Index(s, pendingPhotoPathMarker); i >= 0 {
		rest := strings.TrimSpace(s[i+len(pendingPhotoPathMarker):])
		rest = strings.TrimSuffix(rest, "/")
		if id, err := uuid.Parse(rest); err == nil {
			return id, true
		}
		return uuid.Nil, false
	}
	if id, err := uuid.Parse(s); err == nil {
		return id, true
	}
	return uuid.Nil, false
}

// InsertPendingCargoPhoto inserts metadata after the file was written to disk.
func (r *Repo) InsertPendingCargoPhoto(ctx context.Context, id uuid.UUID, uploaderID *uuid.UUID, mime string, sizeBytes int64, path string) error {
	_, err := r.pg.Exec(ctx, `
INSERT INTO cargo_pending_photos (id, mime, size_bytes, path, uploader_id)
VALUES ($1,$2,$3,$4,$5)`,
		id, mime, sizeBytes, path, uploaderID)
	return err
}

// PendingCargoPhotoExists reports whether a row exists in cargo_pending_photos.
func (r *Repo) PendingCargoPhotoExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var n int
	err := r.pg.QueryRow(ctx, `SELECT 1 FROM cargo_pending_photos WHERE id = $1`, id).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetPendingCargoPhotoByID returns pending photo metadata or nil if not found.
func (r *Repo) GetPendingCargoPhotoByID(ctx context.Context, id uuid.UUID) (*CargoPendingPhoto, error) {
	var p CargoPendingPhoto
	err := r.pg.QueryRow(ctx, `
SELECT id, mime, size_bytes, path, created_at
FROM cargo_pending_photos WHERE id = $1`, id).Scan(
		&p.ID, &p.Mime, &p.SizeBytes, &p.Path, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// ClaimPendingCargoPhoto moves the file from pending storage to cargo photos, inserts cargo_photos, deletes pending row.
func (r *Repo) ClaimPendingCargoPhoto(ctx context.Context, pendingID, cargoID uuid.UUID, uploaderID *uuid.UUID) (uuid.UUID, error) {
	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	var mime string
	var sizeBytes int64
	var oldPath string
	err = tx.QueryRow(ctx, `
SELECT mime, size_bytes, path FROM cargo_pending_photos WHERE id = $1 FOR UPDATE`, pendingID).Scan(&mime, &sizeBytes, &oldPath)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrPendingCargoPhotoNotFound
		}
		return uuid.Nil, err
	}

	root := cargoStorageRoot()
	photoID := uuid.New()
	ext := filepath.Ext(oldPath)
	if ext == "" {
		ext = ".jpg"
	}
	destDir := filepath.Join(root, "cargo", cargoID.String(), "photos")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return uuid.Nil, err
	}
	newPath := filepath.Join(destDir, photoID.String()+ext)

	data, err := os.ReadFile(oldPath)
	if err != nil {
		return uuid.Nil, err
	}
	if err := os.WriteFile(newPath, data, 0o644); err != nil {
		return uuid.Nil, err
	}

	_, err = tx.Exec(ctx, `
INSERT INTO cargo_photos (id, cargo_id, uploader_id, mime, size_bytes, path)
VALUES ($1,$2,$3,$4,$5,$6)`,
		photoID, cargoID, uploaderID, mime, sizeBytes, newPath)
	if err != nil {
		_ = os.Remove(newPath)
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM cargo_pending_photos WHERE id = $1`, pendingID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = os.Remove(newPath)
		return uuid.Nil, err
	}
	_ = os.Remove(oldPath)
	return photoID, nil
}

// SetCargoPhotoURLs replaces cargo.photo_urls (for syncing after uploads).
func (r *Repo) SetCargoPhotoURLs(ctx context.Context, cargoID uuid.UUID, urls []string) error {
	_, err := r.pg.Exec(ctx, `
UPDATE cargo SET photo_urls = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL`, urls, cargoID)
	return err
}
