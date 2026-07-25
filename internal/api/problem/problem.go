// Package problem implements RFC 9457 application/problem+json responses.
package problem

import (
	"encoding/json"
	"net/http"
)

type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

func Status(status int, title, detail string) Problem {
	return Problem{Type: "about:blank", Title: title, Status: status, Detail: detail}
}

func Internal() Problem {
	return Status(http.StatusInternalServerError, "Erreur interne", "Une erreur inattendue est survenue.")
}

func Write(w http.ResponseWriter, r *http.Request, p Problem) {
	p.Instance = r.URL.Path
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
