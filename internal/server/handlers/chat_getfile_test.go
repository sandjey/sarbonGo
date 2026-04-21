package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"sarbonNew/internal/chat"
	"sarbonNew/internal/server/mw"
)

func testAttachmentWithMainAndThumb(t *testing.T) (*chat.Attachment, string, string) {
	t.Helper()
	root := t.TempDir()
	mainPath := filepath.Join(root, "main.jpg")
	thumbPath := filepath.Join(root, "thumb.jpg")
	if err := os.WriteFile(mainPath, []byte("main"), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	if err := os.WriteFile(thumbPath, []byte("thumb"), 0o644); err != nil {
		t.Fatalf("write thumb: %v", err)
	}
	mainID := uuid.New()
	thumbID := uuid.New()
	att := &chat.Attachment{
		ID:               uuid.New(),
		ConversationID:   uuid.New(),
		UploaderID:       uuid.New(),
		Path:             mainPath,
		ThumbPath:        &thumbPath,
		MediaFileID:      &mainID,
		ThumbMediaFileID: &thumbID,
		Mime:             "image/jpeg",
	}
	return att, mainPath, thumbPath
}

func makeGetFileRouter(h *ChatHandler, userID uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/chat/files/:id", func(c *gin.Context) {
		c.Set(mw.CtxUserID, userID)
		h.GetFile(c)
	})
	return r
}

func TestGetFile_OK_Main(t *testing.T) {
	att, mainPath, _ := testAttachmentWithMainAndThumb(t)
	h := &ChatHandler{
		logger: zap.NewNop(),
		getAttachmentForUserFn: func(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error) {
			return att, nil
		},
		getMediaFileByIDFn: func(ctx context.Context, id uuid.UUID) (*chat.MediaFile, error) {
			return &chat.MediaFile{ID: id, Path: mainPath, ContentHash: "abc"}, nil
		},
	}
	userID := uuid.New()
	r := makeGetFileRouter(h, userID)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/files/"+att.ID.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetFile_NotFound_InDB(t *testing.T) {
	h := &ChatHandler{
		logger: zap.NewNop(),
		getAttachmentForUserFn: func(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error) {
			return nil, pgx.ErrNoRows
		},
	}
	userID := uuid.New()
	fileID := uuid.New()
	r := makeGetFileRouter(h, userID)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/files/"+fileID.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetFile_OtherUser_TreatedAsNotFound(t *testing.T) {
	h := &ChatHandler{
		logger: zap.NewNop(),
		getAttachmentForUserFn: func(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error) {
			return nil, pgx.ErrNoRows
		},
	}
	userID := uuid.New()
	fileID := uuid.New()
	r := makeGetFileRouter(h, userID)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/files/"+fileID.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetFile_ThumbExists(t *testing.T) {
	att, _, thumbPath := testAttachmentWithMainAndThumb(t)
	h := &ChatHandler{
		logger: zap.NewNop(),
		getAttachmentForUserFn: func(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error) {
			return att, nil
		},
		getMediaFileByIDFn: func(ctx context.Context, id uuid.UUID) (*chat.MediaFile, error) {
			return &chat.MediaFile{ID: id, Path: thumbPath, ContentHash: "thumbhash"}, nil
		},
	}
	userID := uuid.New()
	r := makeGetFileRouter(h, userID)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/files/"+att.ID.String()+"?thumb=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetFile_ThumbMissing(t *testing.T) {
	att, mainPath, _ := testAttachmentWithMainAndThumb(t)
	att.ThumbMediaFileID = nil
	att.ThumbPath = nil
	h := &ChatHandler{
		logger: zap.NewNop(),
		getAttachmentForUserFn: func(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error) {
			return att, nil
		},
		getMediaFileByIDFn: func(ctx context.Context, id uuid.UUID) (*chat.MediaFile, error) {
			return &chat.MediaFile{ID: id, Path: mainPath, ContentHash: "main"}, nil
		},
	}
	userID := uuid.New()
	r := makeGetFileRouter(h, userID)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/files/"+att.ID.String()+"?thumb=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetFile_DiskMissing(t *testing.T) {
	att := &chat.Attachment{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		UploaderID:     uuid.New(),
		Path:           "missing/path/file.jpg",
		Mime:           "image/jpeg",
	}
	mainID := uuid.New()
	att.MediaFileID = &mainID
	h := &ChatHandler{
		logger: zap.NewNop(),
		getAttachmentForUserFn: func(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error) {
			return att, nil
		},
		getMediaFileByIDFn: func(ctx context.Context, id uuid.UUID) (*chat.MediaFile, error) {
			return &chat.MediaFile{ID: id, Path: "missing/path/file.jpg", ContentHash: "h"}, nil
		},
	}
	userID := uuid.New()
	r := makeGetFileRouter(h, userID)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/files/"+att.ID.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetFile_DBErrorStillNotFoundShape(t *testing.T) {
	h := &ChatHandler{
		logger: zap.NewNop(),
		getAttachmentForUserFn: func(ctx context.Context, attachmentID, userID uuid.UUID) (*chat.Attachment, error) {
			return nil, errors.New("db down")
		},
	}
	userID := uuid.New()
	fileID := uuid.New()
	r := makeGetFileRouter(h, userID)
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/files/"+fileID.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

