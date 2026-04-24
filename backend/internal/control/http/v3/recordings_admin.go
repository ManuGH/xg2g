// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"golang.org/x/sys/unix"
)

const recordingRenameBodyLimit = 4096

type renameRecordingRequest struct {
	Title string `json:"title"`
}

type recordingRenameOp struct {
	oldPath string
	newPath string
}

func (s *Server) PostRecordingDelete(w http.ResponseWriter, r *http.Request, recordingId string) {
	deps := s.recordingsModuleDeps()
	serviceRef, ok := s.requireManagedRecordingOperation(w, r, recordingId)
	if !ok {
		return
	}

	if localPath, err := resolveRecordingThumbnailSourcePath(deps, serviceRef); err == nil {
		if err := ensureLocalRecordingAdminWritable(localPath); err == nil {
			if err := deleteLocalRecordingArtifacts(localPath); err != nil {
				s.writeRecordingAdminDeleteError(w, r, err)
				return
			}
			purgeRecordingCacheDir(deps.cfg.HLS.Root, serviceRef)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		log.L().Info().Str("recordingId", recordingId).Str("serviceRef", serviceRef).Msg("local recording delete unavailable; falling back to receiver delete")
	}

	if deps.recordingsService == nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Recordings service not available", nil)
		return
	}

	if _, err := deps.recordingsService.Delete(r.Context(), recservice.DeleteInput{RecordingID: recordingId}); err != nil {
		s.writeRecordingError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) PostRecordingRename(w http.ResponseWriter, r *http.Request, recordingId string) {
	deps := s.recordingsModuleDeps()
	serviceRef, ok := s.requireManagedRecordingOperation(w, r, recordingId)
	if !ok {
		return
	}

	req, err := decodeRenameRecordingRequest(r)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	title, err := normalizeRecordingRenameTitle(req.Title)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	localPath, err := resolveRecordingThumbnailSourcePath(deps, serviceRef)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording not found", nil)
			return
		}
		writeRegisteredProblem(w, r, http.StatusUnprocessableEntity, "recordings/rename-unsupported", "Rename Unsupported", problemcode.CodeUpdateFailed, "Renaming recordings requires a writable locally mapped recording path.", nil)
		return
	}

	if err := ensureLocalRecordingAdminWritable(localPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording not found", nil)
			return
		}
		writeRegisteredProblem(w, r, http.StatusUnprocessableEntity, "recordings/rename-unsupported", "Rename Unsupported", problemcode.CodeUpdateFailed, "Renaming recordings requires a writable locally mapped recording path.", nil)
		return
	}

	if err := renameLocalRecordingArtifacts(localPath, title); err != nil {
		s.writeRecordingAdminRenameError(w, r, err)
		return
	}

	purgeRecordingCacheDir(deps.cfg.HLS.Root, serviceRef)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requireManagedRecordingOperation(w http.ResponseWriter, r *http.Request, recordingId string) (string, bool) {
	profile, ok := s.requireHouseholdDVRManageAccess(w, r)
	if !ok {
		return "", false
	}

	serviceRef, decoded := recservice.DecodeRecordingID(recordingId)
	if !decoded {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", problemcode.CodeInvalidInput, "invalid recording ID", nil)
		return "", false
	}

	profile = household.NormalizeProfile(profile)
	if !household.IsServiceAllowedNormalized(profile, serviceRef, "") {
		writeHouseholdForbidden(w, r, "household/recording_manage_forbidden", "Recording Manage Forbidden", "The active household profile is not allowed to manage this recording")
		return "", false
	}

	return serviceRef, true
}

func decodeRenameRecordingRequest(r *http.Request) (renameRecordingRequest, error) {
	defer func() {
		_ = r.Body.Close()
	}()

	decoder := json.NewDecoder(io.LimitReader(r.Body, recordingRenameBodyLimit))
	decoder.DisallowUnknownFields()

	var req renameRecordingRequest
	if err := decoder.Decode(&req); err != nil {
		return renameRecordingRequest{}, fmt.Errorf("invalid rename body")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return renameRecordingRequest{}, fmt.Errorf("invalid rename body")
	}

	return req, nil
}

func recordingLocalAdminWritable(deps recordingsModuleDeps, serviceRef string) bool {
	localPath, err := resolveRecordingThumbnailSourcePath(deps, serviceRef)
	if err != nil {
		return false
	}
	return ensureLocalRecordingAdminWritable(localPath) == nil
}

func ensureLocalRecordingAdminWritable(localPath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("recording path is a directory")
	}

	dirPath := filepath.Dir(localPath)
	if err := unix.Access(dirPath, unix.W_OK|unix.X_OK); err != nil {
		switch {
		case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
			return fs.ErrPermission
		case errors.Is(err, unix.ENOENT):
			return fs.ErrNotExist
		default:
			return fmt.Errorf("check recording directory access: %w", err)
		}
	}

	return nil
}

func normalizeRecordingRenameTitle(raw string) (string, error) {
	title := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	switch {
	case title == "":
		return "", fmt.Errorf("title must not be empty")
	case title == "." || title == "..":
		return "", fmt.Errorf("title is invalid")
	case strings.Contains(title, "/"):
		return "", fmt.Errorf("title must not contain path separators")
	case strings.ContainsRune(title, '\x00'):
		return "", fmt.Errorf("title contains invalid characters")
	default:
		return title, nil
	}
}

func deleteLocalRecordingArtifacts(localPath string) error {
	artifacts, err := collectLocalRecordingArtifacts(localPath)
	if err != nil {
		return err
	}

	for _, artifactPath := range artifacts {
		if err := os.Remove(artifactPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove recording artifact %s: %w", artifactPath, err)
		}
	}

	return nil
}

func renameLocalRecordingArtifacts(localPath, title string) error {
	artifacts, err := collectLocalRecordingArtifacts(localPath)
	if err != nil {
		return err
	}

	ops, err := planLocalRecordingRenames(localPath, artifacts, title)
	if err != nil {
		return err
	}

	if len(ops) > 0 {
		if err := ensureRecordingRenameTargetsAvailable(ops); err != nil {
			return err
		}
	}

	if err := updateRecordingMetaTitle(localPath, title); err != nil {
		return err
	}

	if len(ops) == 0 {
		return nil
	}

	for _, op := range ops {
		if err := os.Rename(op.oldPath, op.newPath); err != nil {
			return fmt.Errorf("rename recording artifact %s -> %s: %w", op.oldPath, op.newPath, err)
		}
	}

	return nil
}

func collectLocalRecordingArtifacts(localPath string) ([]string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("recording path is a directory")
	}

	dir := filepath.Dir(localPath)
	baseName := filepath.Base(localPath)
	stemName := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read recording directory: %w", err)
	}

	artifacts := make([]string, 0, 8)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == baseName || strings.HasPrefix(name, baseName+".") || name == stemName+".eit" {
			artifacts = append(artifacts, filepath.Join(dir, name))
		}
	}

	if len(artifacts) == 0 {
		return []string{localPath}, nil
	}

	sort.Strings(artifacts)
	return artifacts, nil
}

func planLocalRecordingRenames(localPath string, artifacts []string, title string) ([]recordingRenameOp, error) {
	baseName := filepath.Base(localPath)
	ext := filepath.Ext(baseName)
	baseStem := strings.TrimSuffix(baseName, ext)
	newBaseName := buildRenamedRecordingBaseName(baseName, title)
	newBaseStem := strings.TrimSuffix(newBaseName, ext)

	ops := make([]recordingRenameOp, 0, len(artifacts))
	for _, artifactPath := range artifacts {
		oldName := filepath.Base(artifactPath)
		var newName string

		switch {
		case oldName == baseName:
			newName = newBaseName
		case strings.HasPrefix(oldName, baseName+"."):
			newName = newBaseName + strings.TrimPrefix(oldName, baseName)
		case oldName == baseStem+".eit":
			newName = newBaseStem + ".eit"
		default:
			continue
		}

		newPath := filepath.Join(filepath.Dir(artifactPath), newName)
		if artifactPath == newPath {
			continue
		}
		ops = append(ops, recordingRenameOp{
			oldPath: artifactPath,
			newPath: newPath,
		})
	}

	return ops, nil
}

func buildRenamedRecordingBaseName(baseName, title string) string {
	ext := filepath.Ext(baseName)
	stem := strings.TrimSuffix(baseName, ext)
	if idx := strings.LastIndex(stem, " - "); idx >= 0 {
		return stem[:idx+3] + title + ext
	}
	return title + ext
}

func ensureRecordingRenameTargetsAvailable(ops []recordingRenameOp) error {
	sourcePaths := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		sourcePaths[filepath.Clean(op.oldPath)] = struct{}{}
	}

	for _, op := range ops {
		targetPath := filepath.Clean(op.newPath)
		if _, ok := sourcePaths[targetPath]; ok {
			continue
		}
		if _, err := os.Stat(targetPath); err == nil {
			return fmt.Errorf("rename target exists: %w", fs.ErrExist)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat rename target: %w", err)
		}
	}

	return nil
}

func updateRecordingMetaTitle(localPath, title string) error {
	metaPath := localPath + ".meta"
	// #nosec G304 -- metaPath is derived from a validated local recording path under operator-controlled storage mappings.
	content, err := os.ReadFile(filepath.Clean(metaPath))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read recording meta: %w", err)
	}

	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	switch {
	case len(lines) == 0:
		lines = []string{"", title}
	case len(lines) == 1:
		lines = append(lines, title)
	default:
		lines[1] = title
	}

	updated := strings.Join(lines, "\n")
	if len(content) > 0 && content[len(content)-1] == '\n' && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}

	mode := fs.FileMode(0o644)
	if info, statErr := os.Stat(metaPath); statErr == nil {
		mode = info.Mode().Perm()
	}

	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(updated), mode); err != nil {
		return fmt.Errorf("write recording meta: %w", err)
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace recording meta: %w", err)
	}

	return nil
}

func purgeRecordingCacheDir(hlsRoot, serviceRef string) {
	cacheDir, err := recservice.RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		log.L().Debug().Err(err).Str("cacheDir", cacheDir).Msg("failed to purge recording cache")
	}
}

func (s *Server) writeRecordingAdminDeleteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording not found", nil)
	case errors.Is(err, fs.ErrPermission):
		log.L().Warn().Err(err).Msg("recording delete permission failure")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "recordings/delete-failed", "Delete Failed", problemcode.CodeDeleteFailed, "The recording could not be deleted from the local filesystem.", nil)
	default:
		log.L().Warn().Err(err).Msg("recording delete failed")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "recordings/delete-failed", "Delete Failed", problemcode.CodeDeleteFailed, "The recording could not be deleted.", nil)
	}
}

func (s *Server) writeRecordingAdminRenameError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording not found", nil)
	case errors.Is(err, fs.ErrExist):
		writeRegisteredProblem(w, r, http.StatusConflict, "recordings/rename-conflict", "Rename Conflict", problemcode.CodeConflict, "A recording with the target name already exists.", nil)
	case errors.Is(err, fs.ErrPermission):
		log.L().Warn().Err(err).Msg("recording rename permission failure")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "recordings/rename-failed", "Rename Failed", problemcode.CodeUpdateFailed, "The recording could not be renamed on the local filesystem.", nil)
	default:
		log.L().Warn().Err(err).Msg("recording rename failed")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "recordings/rename-failed", "Rename Failed", problemcode.CodeUpdateFailed, "The recording could not be renamed.", nil)
	}
}
