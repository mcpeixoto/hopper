package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/mcpeixoto/hopper/internal/cron"
	"github.com/mcpeixoto/hopper/internal/store"
)

type scheduleRequest struct {
	Name string        `json:"name"`
	Cron string        `json:"cron"`
	Spec store.JobSpec `json:"spec"`
}

// CreateSchedule registers a recurring job. Operator auth.
func (a *API) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if !readJSON(w, r, &req) {
		return
	}
	sched, err := cron.Parse(req.Cron)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Spec.Image == "" {
		writeError(w, http.StatusBadRequest, "spec.image is required")
		return
	}
	next := sched.Next(time.Now().UTC())
	sc, err := a.Store.CreateSchedule(req.Name, req.Cron, req.Spec, next.Format(time.RFC3339))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sc)
}

// ListSchedules returns all recurring jobs. Operator auth.
func (a *API) ListSchedules(w http.ResponseWriter, r *http.Request) {
	scs, err := a.Store.ListSchedules()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if scs == nil {
		scs = []store.Schedule{}
	}
	writeJSON(w, http.StatusOK, scs)
}

// DeleteSchedule removes a recurring job. Operator auth.
func (a *API) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteSchedule(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "schedule not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
