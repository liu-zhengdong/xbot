package account

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strings"
)

// Prefix is where the Account API lives, standalone or inside xbot serve.
const Prefix = "/account/v1/"

// LinkPath is the page a mail link opens.
const LinkPath = Prefix + "link"

const maxBody = 16 << 10

// Handler serves the Account API. Bearer sessions; JSON in and out; the mail
// link page is HTML and confirms only on a button press, so a mail scanner
// that fetches the link does not use it up.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+Prefix+"register", s.hRegister)
	mux.HandleFunc("POST "+Prefix+"login", s.hLogin)
	mux.HandleFunc("POST "+Prefix+"magic", s.hMagic)
	mux.HandleFunc("POST "+Prefix+"resend", s.hResend)
	mux.HandleFunc("POST "+Prefix+"code", s.hCode)
	mux.HandleFunc("POST "+Prefix+"poll", s.hPoll)
	mux.HandleFunc("GET "+LinkPath, s.hLinkPage)
	mux.HandleFunc("POST "+LinkPath, s.hLinkConfirm)
	mux.HandleFunc("GET "+Prefix+"me", s.hMe)
	mux.HandleFunc("POST "+Prefix+"logout", s.hLogout)
	mux.HandleFunc("GET "+Prefix+"devices", s.hDevices)
	mux.HandleFunc("POST "+Prefix+"devices", s.hReport)
	mux.HandleFunc("GET "+Prefix+"health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

// ErrorCode maps an error to the API's error string.
func ErrorCode(err error) (int, string) {
	switch {
	case errors.Is(err, ErrInput):
		return http.StatusBadRequest, "bad_input"
	case errors.Is(err, ErrCredentials):
		return http.StatusUnauthorized, "credentials"
	case errors.Is(err, ErrUnverified):
		return http.StatusForbidden, "unverified"
	case errors.Is(err, ErrBusy):
		return http.StatusTooManyRequests, "busy"
	case errors.Is(err, ErrExpired):
		return http.StatusGone, "expired"
	case errors.Is(err, ErrSession):
		return http.StatusUnauthorized, "session"
	default:
		return http.StatusInternalServerError, "internal"
	}
}

// CodeError turns an API error string back into the sentinel.
func CodeError(code string) error {
	switch code {
	case "bad_input":
		return ErrInput
	case "credentials":
		return ErrCredentials
	case "unverified":
		return ErrUnverified
	case "busy":
		return ErrBusy
	case "expired":
		return ErrExpired
	case "session":
		return ErrSession
	default:
		return errors.New("account: service error")
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code, msg := ErrorCode(err)
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return ErrInput
	}
	return nil
}

func bearer(r *http.Request) string {
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return strings.TrimSpace(tok)
}

type credsIn struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type challengeIn struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code,omitempty"`
}

func (s *Service) hRegister(w http.ResponseWriter, r *http.Request) {
	// Counted before the body is read, so malformed requests use up the hour too.
	if !s.regLimit.allow(clientIP(r), s.cfg.Now()) {
		writeErr(w, ErrBusy)
		return
	}
	var in credsIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.register(r.Context(), in.Email, in.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) hLogin(w http.ResponseWriter, r *http.Request) {
	var in credsIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	signed, pend, err := s.Login(r.Context(), in.Email, in.Password)
	if errors.Is(err, ErrUnverified) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unverified", "challenge": pend.Challenge, "email": pend.Email})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, signed)
}

func (s *Service) hMagic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.Magic(r.Context(), in.Email)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) hResend(w http.ResponseWriter, r *http.Request) {
	var in challengeIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Resend(r.Context(), in.Challenge); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) hCode(w http.ResponseWriter, r *http.Request) {
	var in challengeIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.ConfirmCode(in.Challenge, in.Code)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) hPoll(w http.ResponseWriter, r *http.Request) {
	var in challengeIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.Poll(in.Challenge)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) hMe(w http.ResponseWriter, r *http.Request) {
	out, err := s.Session(bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) hLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.Logout(bearer(r)); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) hDevices(w http.ResponseWriter, r *http.Request) {
	out, err := s.Devices(bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Service) hReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   string   `json:"id"`
		Name string   `json:"name"`
		Bots []string `json:"bots"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.ReportDevice(bearer(r), in.ID, in.Name, in.Bots); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

var linkPage = template.Must(template.New("link").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{.Title}}</title>
<style>
:root{--bg:#f3f3f5;--card:#fff;--text:#18181b;--muted:#6b6b70;--pri:#18181b;--on:#fff;color-scheme:light}
@media (prefers-color-scheme:dark){:root{--bg:#0f0f11;--card:#18181b;--text:#ececef;--muted:#a1a1a9;--pri:#f2f2f2;--on:#111;color-scheme:dark}}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--text);font-family:"Segoe UI","Noto Sans SC","PingFang SC",sans-serif;padding:16px}
main{width:100%;max-width:420px;background:var(--card);border-radius:16px;padding:28px}
h1{font-size:22px;margin:0 0 8px}p{color:var(--muted);font-size:14px;line-height:1.6;margin:0 0 20px}
button{width:100%;height:44px;border:0;border-radius:12px;background:var(--pri);color:var(--on);font:inherit;font-size:15px;font-weight:600;cursor:pointer}
</style></head><body><main>
<h1>{{.Title}}</h1><p>{{.Text}}</p>
{{if .Token}}<form method="post"><input type="hidden" name="t" value="{{.Token}}"><button type="submit">确认</button></form>{{end}}
</main></body></html>`))

type linkView struct{ Title, Text, Token string }

func renderLink(w http.ResponseWriter, code int, v linkView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(code)
	_ = linkPage.Execute(w, v)
}

func (s *Service) hLinkPage(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("t")
	if !s.LinkValid(tok) {
		renderLink(w, http.StatusGone, linkView{Title: "链接已失效", Text: "这个链接过期或已经用过了。回到 XBot 再发一次邮件。"})
		return
	}
	renderLink(w, http.StatusOK, linkView{Title: "确认登录 XBot", Text: "点「确认」后，刚才在 XBot 里等待的那个窗口就会自动进入。不是你本人操作，关掉这个页面即可。", Token: tok})
}

func (s *Service) hLinkConfirm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		renderLink(w, http.StatusBadRequest, linkView{Title: "链接已失效", Text: "回到 XBot 再发一次邮件。"})
		return
	}
	if err := s.ConfirmLink(r.PostForm.Get("t")); err != nil {
		renderLink(w, http.StatusGone, linkView{Title: "链接已失效", Text: "这个链接过期或已经用过了。回到 XBot 再发一次邮件。"})
		return
	}
	renderLink(w, http.StatusOK, linkView{Title: "已完成", Text: "回到 XBot，它会自动继续。这个页面可以关掉了。"})
}
