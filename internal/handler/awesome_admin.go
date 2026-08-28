package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

const maxAwesomeAdminBody = 64 * 1024

// AwesomeAdminService is isolated from public reads and is only mounted behind Admin auth.
type AwesomeAdminService interface {
	CreateSource(context.Context, model.AwesomeSource) (model.AwesomeSource, error)
	UpdateSource(context.Context, model.AwesomeSource, int) (model.AwesomeSource, error)
	ListSources(context.Context) ([]model.AwesomeSource, error)
	SyncSource(context.Context, string, string) (model.AwesomeSyncRun, error)
	PublishSource(context.Context, string) (model.AwesomeSource, error)
	ArchiveSource(context.Context, string) (model.AwesomeSource, error)
	SyncRuns(context.Context, string) ([]model.AwesomeSyncRun, error)
}

type AwesomeAdminHandler struct {
	service AwesomeAdminService
}

func NewAwesomeAdminHandler(service AwesomeAdminService) *AwesomeAdminHandler {
	return &AwesomeAdminHandler{service: service}
}

func (h *AwesomeAdminHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	sources, err := h.service.ListSources(r.Context())
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	WriteJSONWithMeta(w, sources, &model.Meta{Total: len(sources)})
}

func (h *AwesomeAdminHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var request awesomeSourceWriteRequest
	if !decodeAwesomeAdminBody(w, r, &request) {
		return
	}
	source, err := h.service.CreateSource(r.Context(), request.source(r.PathValue("source_id")))
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	WriteJSON(w, source)
}

func (h *AwesomeAdminHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var request awesomeSourceWriteRequest
	if !decodeAwesomeAdminBody(w, r, &request) {
		return
	}
	request.ID = r.PathValue("source_id")
	source, err := h.service.UpdateSource(r.Context(), request.source(request.ID), request.Revision)
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	WriteJSON(w, source)
}

func (h *AwesomeAdminHandler) HandleSync(w http.ResponseWriter, r *http.Request) {
	// Use a detached context so closing the local admin page cannot cancel a persisted run.
	run, err := h.service.SyncSource(context.Background(), r.PathValue("source_id"), "manual")
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	WriteJSON(w, run)
}

func (h *AwesomeAdminHandler) HandlePublish(w http.ResponseWriter, r *http.Request) {
	source, err := h.service.PublishSource(r.Context(), r.PathValue("source_id"))
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	WriteJSON(w, source)
}

func (h *AwesomeAdminHandler) HandleArchive(w http.ResponseWriter, r *http.Request) {
	source, err := h.service.ArchiveSource(r.Context(), r.PathValue("source_id"))
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	WriteJSON(w, source)
}

func (h *AwesomeAdminHandler) HandleSyncRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.service.SyncRuns(r.Context(), r.PathValue("source_id"))
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	WriteJSONWithMeta(w, runs, &model.Meta{Total: len(runs)})
}

type awesomeSourceWriteRequest struct {
	ID            string                     `json:"id"`
	RepoFullName  string                     `json:"repo_full_name"`
	DisplayName   string                     `json:"display_name"`
	ImageURL      string                     `json:"image_url"`
	SummaryZH     string                     `json:"summary_zh"`
	SummaryEN     string                     `json:"summary_en"`
	Featured      bool                       `json:"featured"`
	SortOrder     int                        `json:"sort_order"`
	ParserProfile model.AwesomeParserProfile `json:"parser_profile"`
	Revision      int                        `json:"revision"`
}

func (r awesomeSourceWriteRequest) source(id string) model.AwesomeSource {
	if id == "" {
		id = r.ID
	}
	return model.AwesomeSource{
		ID: id, RepoFullName: r.RepoFullName, DisplayName: r.DisplayName, ImageURL: r.ImageURL,
		SummaryZH: r.SummaryZH, SummaryEN: r.SummaryEN, Featured: r.Featured, SortOrder: r.SortOrder,
		ParserProfile: r.ParserProfile,
	}
}

func decodeAwesomeAdminBody(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAwesomeAdminBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		WriteError(w, http.StatusBadRequest, "AWESOME_SOURCE_INVALID", "请求字段无效", nil)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "AWESOME_SOURCE_INVALID", "请求只能包含一个 JSON 对象", nil)
		return false
	}
	return true
}
