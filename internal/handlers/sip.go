package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rkislov/pomogayka/internal/middleware"
	"github.com/rkislov/pomogayka/internal/models"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) AdminSIPSave(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	existing, _ := a.Store.GetSIPSettings(user.TenantID)
	turnPass := r.FormValue("turn_password")
	if turnPass == "" && existing != nil {
		turnPass = existing.TURNPassword
	}
	cfg := models.SIPSettings{
		TenantID:      user.TenantID,
		Enabled:       r.FormValue("enabled") == "1",
		WebsocketURL:  strings.TrimSpace(r.FormValue("websocket_url")),
		SIPDomain:     strings.TrimSpace(r.FormValue("sip_domain")),
		OutboundProxy: strings.TrimSpace(r.FormValue("outbound_proxy")),
		STUNURLs:      strings.TrimSpace(r.FormValue("stun_urls")),
		TURNURLs:      strings.TrimSpace(r.FormValue("turn_urls")),
		TURNUsername:  strings.TrimSpace(r.FormValue("turn_username")),
		TURNPassword:  turnPass,
	}
	if err := a.Store.SaveSIPSettings(cfg); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", "SIP: "+err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "Настройки SIP сохранены")
	}
	http.Redirect(w, r, "/admin#sip", http.StatusSeeOther)
}

func (a *App) APISoftphoneConfig(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil || !user.Role.IsStaff() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		return
	}
	sip, err := a.Store.GetSIPSettings(user.TenantID)
	if err != nil || !sip.IsConfigured() {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	cred, _ := a.Store.GetUserSIPCredential(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        true,
		"websocket_url":  sip.WebsocketURL,
		"sip_domain":     sip.SIPDomain,
		"outbound_proxy": sip.OutboundProxy,
		"stun_urls":      splitCSV(sip.STUNURLs),
		"turn_urls":      splitCSV(sip.TURNURLs),
		"turn_username":  sip.TURNUsername,
		"turn_password":  sip.TURNPassword,
		"extension":      cred.Extension,
		"auth_username":  cred.AuthUsername,
		"password":       cred.Password,
		"display_name":   firstNonEmpty(cred.DisplayName, user.FullName),
		"auto_register":  cred.AutoRegister,
		"agent_name":     user.FullName,
	})
}

func (a *App) APISoftphoneCredentials(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil || !user.Role.IsStaff() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		return
	}
	_ = r.ParseForm()
	c := models.UserSIPCredential{
		UserID:       user.ID,
		TenantID:     user.TenantID,
		Extension:    strings.TrimSpace(r.FormValue("extension")),
		AuthUsername: strings.TrimSpace(r.FormValue("auth_username")),
		Password:     r.FormValue("password"),
		DisplayName:  strings.TrimSpace(r.FormValue("display_name")),
		AutoRegister: r.FormValue("auto_register") == "1",
	}
	if c.Extension == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "укажите SIP-логин / extension"})
		return
	}
	if err := a.Store.SaveUserSIPCredential(c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) APICallScreenPop(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil || !user.Role.IsStaff() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		return
	}
	_ = r.ParseForm()
	phone := strings.TrimSpace(r.FormValue("phone"))
	if phone == "" {
		phone = strings.TrimSpace(r.URL.Query().Get("phone"))
	}
	callerName := strings.TrimSpace(r.FormValue("caller_name"))
	if callerName == "" {
		callerName = strings.TrimSpace(r.URL.Query().Get("caller_name"))
	}

	var caller any
	requesterID := ""
	if cu, err := a.Store.FindUserByPhone(user.TenantID, phone); err == nil {
		requesterID = cu.ID
		caller = map[string]any{
			"id":        cu.ID,
			"full_name": cu.FullName,
			"email":     cu.Email,
			"role":      cu.Role,
		}
		if callerName == "" {
			callerName = cu.FullName
		}
	}

	title := "Входящий звонок"
	if phone != "" {
		title += " " + phone
	}
	if callerName != "" {
		title += " · " + callerName
	}
	desc := "Заявка создана при принятии входящего звонка."
	if phone != "" {
		desc += "\nТелефон: " + phone
	}
	if callerName != "" {
		desc += "\nАбонент: " + callerName
	}

	q := url.Values{}
	q.Set("from_call", "1")
	if phone != "" {
		q.Set("phone", phone)
	}
	if requesterID != "" {
		q.Set("requester_id", requesterID)
	}
	q.Set("title", title)
	q.Set("description", desc)
	redirect := "/tickets/new?" + q.Encode()

	writeJSON(w, http.StatusOK, map[string]any{
		"phone":       phone,
		"caller":      caller,
		"redirect":    redirect,
		"title":       title,
		"description": desc,
	})
}

func (a *App) Clients(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	clients, _ := a.Store.ListClientsWithPhones(user.TenantID)
	type row struct {
		User   models.User
		Phones []models.UserPhone
	}
	rows := make([]row, 0, len(clients))
	for _, c := range clients {
		phones, _ := a.Store.ListUserPhones(c.ID)
		rows = append(rows, row{User: c, Phones: phones})
	}
	a.Render.Render(w, r, "pages/clients.html", pageData{
		"Title": "Клиенты и телефоны", "Rows": rows,
		"Flash": a.Sessions.PopString(r.Context(), "flash"),
		"Error": a.Sessions.PopString(r.Context(), "flash_error"),
	})
}

func (a *App) ClientPhoneAdd(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	_ = r.ParseForm()
	clientID := chi.URLParam(r, "id")
	client, err := a.Store.GetUserByID(clientID)
	if err != nil || client.TenantID != user.TenantID || client.Role != models.RoleClient {
		http.Error(w, "Клиент не найден", http.StatusNotFound)
		return
	}
	phone := strings.TrimSpace(r.FormValue("phone"))
	label := strings.TrimSpace(r.FormValue("label"))
	primary := r.FormValue("is_primary") == "1"
	if _, err := a.Store.AddUserPhone(user.TenantID, clientID, phone, label, models.PhoneSourceManual, primary); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", "Не удалось добавить номер (возможно, уже занят): "+err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "Номер добавлен")
	}
	http.Redirect(w, r, "/clients", http.StatusSeeOther)
}

func (a *App) ClientPhoneDelete(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	phoneID := chi.URLParam(r, "phoneID")
	if err := a.Store.DeleteUserPhone(user.TenantID, phoneID); err != nil {
		a.Sessions.Put(r.Context(), "flash_error", err.Error())
	} else {
		a.Sessions.Put(r.Context(), "flash", "Номер удалён")
	}
	http.Redirect(w, r, "/clients", http.StatusSeeOther)
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
