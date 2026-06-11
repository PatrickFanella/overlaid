package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

type setOBSSceneRequest struct {
	SceneName string `json:"sceneName"`
}

type setOBSInputMuteRequest struct {
	InputName string `json:"inputName"`
	Muted     bool   `json:"muted"`
}

type setOBSSceneItemEnabledRequest struct {
	SceneName   string `json:"sceneName"`
	SceneItemID int    `json:"sceneItemId"`
	Enabled     bool   `json:"enabled"`
}

func (s *Server) obsStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.obs.Status(r.Context()))
}

func (s *Server) obsScenes(w http.ResponseWriter, r *http.Request) {
	scenes, err := s.obs.GetSceneList(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, scenes)
}

func (s *Server) obsInputs(w http.ResponseWriter, r *http.Request) {
	inputs, err := s.obs.GetInputList(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, inputs)
}

func (s *Server) obsSceneItems(w http.ResponseWriter, r *http.Request) {
	sceneName := strings.TrimSpace(r.URL.Query().Get("sceneName"))
	if sceneName == "" {
		http.Error(w, "sceneName query parameter is required", http.StatusBadRequest)
		return
	}

	items, err := s.obs.GetSceneItemList(r.Context(), sceneName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, items)
}

func (s *Server) obsSetScene(w http.ResponseWriter, r *http.Request) {
	var request setOBSSceneRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.obs.SetCurrentProgramScene(r.Context(), request.SceneName); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) obsSetInputMute(w http.ResponseWriter, r *http.Request) {
	var request setOBSInputMuteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.obs.SetInputMute(r.Context(), request.InputName, request.Muted); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) obsSetSceneItemEnabled(w http.ResponseWriter, r *http.Request) {
	var request setOBSSceneItemEnabledRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.obs.SetSceneItemEnabled(r.Context(), request.SceneName, request.SceneItemID, request.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
