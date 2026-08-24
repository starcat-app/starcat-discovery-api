package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/awesome"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

// AwesomePublicService is the read-only contract exposed to Starcat clients.
type AwesomePublicService interface {
	ListPublishedSources(ctx context.Context) ([]model.AwesomeSource, error)
	PublishedEntries(ctx context.Context, sourceID string) (model.AwesomeEntriesSnapshot, error)
}

// AwesomeHandler exposes managed source catalog and verified entries with stable ETags.
type AwesomeHandler struct {
	service AwesomePublicService
}

func NewAwesomeHandler(service AwesomePublicService) *AwesomeHandler {
	return &AwesomeHandler{service: service}
}

func (h *AwesomeHandler) HandleSources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.service.ListPublishedSources(r.Context())
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	cards := make([]model.AwesomeSourceCard, 0, len(sources))
	generatedAt := time.Time{}
	for _, source := range sources {
		cards = append(cards, sourceCard(source))
		if source.UpdatedAt.After(generatedAt) {
			generatedAt = source.UpdatedAt
		}
		if source.LastSyncedAt != nil && source.LastSyncedAt.After(generatedAt) {
			generatedAt = *source.LastSyncedAt
		}
	}
	writeAwesomeCacheable(w, r, cards, &model.Meta{Total: len(cards), GeneratedAt: generatedAt.Format(time.RFC3339)})
}

func (h *AwesomeHandler) HandleEntries(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.service.PublishedEntries(r.Context(), r.PathValue("source_id"))
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	writeAwesomeCacheable(w, r, snapshot, &model.Meta{
		Total: len(snapshot.Entries), GeneratedAt: snapshot.Source.UpdatedAt.Format(time.RFC3339),
	})
}

func sourceCard(source model.AwesomeSource) model.AwesomeSourceCard {
	return model.AwesomeSourceCard{
		ID: source.ID, DisplayName: source.DisplayName, RepoFullName: source.RepoFullName,
		RepoURL: source.RepoURL, ImageURL: source.ImageURL, SummaryZH: source.SummaryZH,
		SummaryEN: source.SummaryEN, Featured: source.Featured, SortOrder: source.SortOrder,
		SourceStars: source.SourceStars, GitHubRepoCount: source.GitHubRepoCount, ExternalEntryCount: source.ExternalEntryCount,
		LastSyncedAt: source.LastSyncedAt, UpdatedAt: source.UpdatedAt,
	}
}

func writeAwesomeCacheable[T any](w http.ResponseWriter, r *http.Request, data T, meta *model.Meta) {
	payload, err := json.Marshal(model.Envelope[T]{SchemaVersion: 1, Data: data, Meta: meta})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "ENCODE_ERROR", "无法编码 Awesome 响应", nil)
		return
	}
	digest := sha256.Sum256(payload)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func writeAwesomeError(w http.ResponseWriter, err error) {
	var serviceErr *awesome.ServiceError
	if errors.As(err, &serviceErr) {
		WriteError(w, serviceErr.Status, serviceErr.Code, serviceErr.Message, nil)
		return
	}
	WriteError(w, http.StatusInternalServerError, "STORE_ERROR", "Awesome 服务暂时不可用", nil)
}
