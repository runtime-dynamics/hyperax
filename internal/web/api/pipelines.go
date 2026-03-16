package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hyperax/hyperax/internal/repo"
)

// PipelineAPI handles REST endpoints for pipelines and jobs.
type PipelineAPI struct {
	repo repo.PipelineRepo
}

func NewPipelineAPI(r repo.PipelineRepo) *PipelineAPI {
	return &PipelineAPI{repo: r}
}

func (a *PipelineAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", a.list)
	r.Post("/", a.create)
	r.Get("/search", a.search)
	r.Get("/{id}", a.get)
	r.Get("/{id}/jobs", a.listJobs)
	r.Get("/{id}/jobs/{jobID}", a.getJob)
	r.Get("/{id}/jobs/{jobID}/steps", a.listSteps)
	return r
}

func (a *PipelineAPI) list(w http.ResponseWriter, r *http.Request) {
	workspace := queryStr(r, "workspace", "")
	pipelines, err := a.repo.ListPipelines(r.Context(), workspace)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, pipelines)
}

func (a *PipelineAPI) get(w http.ResponseWriter, r *http.Request) {
	id := urlParam(r, "id")
	pipeline, err := a.repo.GetPipeline(r.Context(), id)
	if err != nil {
		respondError(w, r,http.StatusNotFound, "pipeline not found")
		return
	}
	respondJSON(w, r,http.StatusOK, pipeline)
}

func (a *PipelineAPI) search(w http.ResponseWriter, r *http.Request) {
	query := queryStr(r, "q", "")
	workspace := queryStr(r, "workspace", "")
	pipelines, err := a.repo.SearchPipelines(r.Context(), query, workspace)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, pipelines)
}

func (a *PipelineAPI) create(w http.ResponseWriter, r *http.Request) {
	var p repo.Pipeline
	if err := decodeBody(r, &p); err != nil {
		respondError(w, r,http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" {
		respondError(w, r,http.StatusBadRequest, "name is required")
		return
	}
	id, err := a.repo.CreatePipeline(r.Context(), &p)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	p.ID = id
	respondJSON(w, r,http.StatusCreated, p)
}

func (a *PipelineAPI) listJobs(w http.ResponseWriter, r *http.Request) {
	pipelineID := urlParam(r, "id")
	status := queryStr(r, "status", "")
	limit := queryInt(r, "limit", 50)
	filter := repo.JobFilter{Status: status, Limit: limit}
	jobs, err := a.repo.ListJobsFiltered(r.Context(), pipelineID, filter)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, jobs)
}

func (a *PipelineAPI) getJob(w http.ResponseWriter, r *http.Request) {
	jobID := urlParam(r, "jobID")
	job, err := a.repo.GetJob(r.Context(), jobID)
	if err != nil {
		respondError(w, r,http.StatusNotFound, "job not found")
		return
	}
	respondJSON(w, r,http.StatusOK, job)
}

func (a *PipelineAPI) listSteps(w http.ResponseWriter, r *http.Request) {
	jobID := urlParam(r, "jobID")
	steps, err := a.repo.ListStepResults(r.Context(), jobID)
	if err != nil {
		respondError(w, r,http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r,http.StatusOK, steps)
}
