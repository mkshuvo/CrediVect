package api

import (
	"encoding/json"
	"net/http"
	"time"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string      `json:"token"`
	User  UserInfo    `json:"user"`
}

type UserInfo struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Role      string `json:"role"` // APPLICANT, UNDERWRITER, COMPLIANCE_ADMIN
}

func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Deterministic role mapping for seamless testing
	role := "APPLICANT"
	name := "Alex"
	lastName := "Chen"
	if req.Email == "underwriter@credivect.com" {
		role = "UNDERWRITER"
		name = "Sarah"
		lastName = "Jenkins"
	} else if req.Email == "admin@credivect.com" {
		role = "COMPLIANCE_ADMIN"
		name = "David"
		lastName = "Vogel"
	}

	resp := AuthResponse{
		Token: "credivect-session-token-" + req.Email + "-" + time.Now().Format("20060102150405"),
		User: UserInfo{
			ID:        "usr-01918a2b-auth",
			Email:     req.Email,
			FirstName: name,
			LastName:  lastName,
			Role:      role,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) HandleMe(w http.ResponseWriter, r *http.Request) {
	resp := UserInfo{
		ID:        "usr-01918a2b-auth",
		Email:     "underwriter@credivect.com",
		FirstName: "Sarah",
		LastName:  "Jenkins",
		Role:      "UNDERWRITER",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
