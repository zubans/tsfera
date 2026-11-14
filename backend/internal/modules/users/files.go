package users

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

// StoredFile описывает загруженный файл в системе (баннеры, QR-оплаты, вложения).
type StoredFile struct {
	ID           int64     `json:"id"`
	OriginalName string    `json:"original_name"`
	MimeType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	StoragePath  string    `json:"-"`
	URL          string    `json:"url"`
	CreatedBy    *int64    `json:"created_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

const maxUploadSizeBytes = 25 << 20 // 25 MiB

// SetFileStorageDir задаёт каталог хранения загруженных файлов пользователей/CRM.
func (s *Service) SetFileStorageDir(dir string) {
	dir = strings.TrimSpace(dir)
	if dir != "" {
		s.fileStorageDir = dir
	}
}

// FileStorageDir возвращает каталог хранения загруженных файлов.
func (s *Service) FileStorageDir() string {
	if strings.TrimSpace(s.fileStorageDir) != "" {
		return s.fileStorageDir
	}
	return filepath.Join(".", "data", "files")
}

// UploadFile сохраняет файл на диск и регистрирует метаданные в users_files.
func (s *Service) UploadFile(ctx context.Context, createdBy *int64, originalName, mimeType string, r io.Reader) (*StoredFile, error) {
	originalName = strings.TrimSpace(filepath.Base(originalName))
	if originalName == "" || originalName == "." || originalName == string(filepath.Separator) {
		originalName = "upload.bin"
	}
	baseDir := s.FileStorageDir()
	subDir := filepath.Join(baseDir, time.Now().Format("2006-01"))
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}

	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(originalName))
	if len(ext) > 16 {
		ext = ""
	}
	fileName := hex.EncodeToString(rnd[:]) + ext
	fullPath := filepath.Join(subDir, fileName)

	dst, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("create upload file: %w", err)
	}
	limited := io.LimitReader(r, maxUploadSizeBytes+1)
	written, copyErr := io.Copy(dst, limited)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(fullPath)
		return nil, fmt.Errorf("write upload file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(fullPath)
		return nil, closeErr
	}
	if written == 0 {
		_ = os.Remove(fullPath)
		return nil, platform.NewValidation("Файл пуст", map[string]string{"file": "Пустой файл"})
	}
	if written > maxUploadSizeBytes {
		_ = os.Remove(fullPath)
		return nil, platform.NewValidation("Файл превышает допустимый размер 25 МБ", map[string]string{"file": "Размер больше 25 МБ"})
	}

	mimeType = strings.TrimSpace(mimeType)
	if mimeType == "" || mimeType == "application/octet-stream" {
		if detected := mime.TypeByExtension(ext); detected != "" {
			mimeType = detected
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	relPath := filepath.Join(time.Now().Format("2006-01"), fileName)
	rec := &StoredFile{
		OriginalName: originalName,
		MimeType:     mimeType,
		SizeBytes:    written,
		StoragePath:  relPath,
		CreatedBy:    createdBy,
	}
	if err := s.repo.CreateFile(ctx, rec); err != nil {
		_ = os.Remove(fullPath)
		return nil, err
	}
	rec.URL = fmt.Sprintf("/api/v1/files/%d", rec.ID)
	return rec, nil
}

// GetFile возвращает метаданные файла и безопасный путь на диске.
func (s *Service) GetFile(ctx context.Context, id int64) (*StoredFile, string, error) {
	if id <= 0 {
		return nil, "", platform.NewValidation("Некорректный ID файла", nil)
	}
	rec, err := s.repo.GetFile(ctx, id)
	if err != nil {
		return nil, "", err
	}
	rec.URL = fmt.Sprintf("/api/v1/files/%d", rec.ID)
	cleanRel := filepath.Clean(rec.StoragePath)
	if filepath.IsAbs(cleanRel) || strings.HasPrefix(cleanRel, "..") {
		return nil, "", platform.NewForbidden("Недопустимый путь к файлу")
	}
	fullPath := filepath.Join(s.FileStorageDir(), cleanRel)
	return rec, fullPath, nil
}

// ListUserIDsByPermission возвращает ID активных пользователей, обладающих правом (или ролью суперадминистратора).
func (s *Service) ListUserIDsByPermission(ctx context.Context, perm string) ([]int64, error) {
	perm = strings.TrimSpace(perm)
	if perm == "" {
		return nil, nil
	}
	return s.repo.ListUserIDsByPermission(ctx, perm)
}

func (h *Handler) uploadFile(w http.ResponseWriter, r *http.Request) {
	ac, ok := platform.AuthFromContext(r.Context())
	if !ok {
		platform.WriteError(w, h.logger, platform.NewUnauthorized("Требуется авторизация"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSizeBytes+1024*1024)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Ожидается multipart/form-data с полем file", nil))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Поле file обязательно", map[string]string{"file": "Файл не выбран"}))
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	rec, err := h.svc.UploadFile(r.Context(), &ac.UserID, header.Filename, contentType, file)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, rec)
}

func (h *Handler) downloadFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный ID файла", nil))
		return
	}
	rec, fullPath, err := h.svc.GetFile(r.Context(), id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	f, err := os.Open(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			platform.WriteError(w, h.logger, platform.NewNotFound("Файл не найден на диске"))
			return
		}
		platform.WriteError(w, h.logger, err)
		return
	}
	defer f.Close()

	if rec.MimeType != "" {
		w.Header().Set("Content-Type", rec.MimeType)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", rec.OriginalName))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, rec.OriginalName, rec.CreatedAt, f)
}
