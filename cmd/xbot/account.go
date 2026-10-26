package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"xbot/internal/account"
)

// cmdAccountAPI runs the Account API on its own, configured from the
// environment, so a host needs only this binary and a data directory.
func cmdAccountAPI(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: xbot account-api (configure with DATA_DIR, BASE_URL, ADDR, MAIL_MODE, MAIL_OUTBOX)")
	}
	env, err := account.EnvFrom(os.Getenv)
	if err != nil {
		return err
	}
	svc, err := account.Open(env)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "account api on %s, links to %s, mail to %s\n", env.Addr, env.BaseURL, env.Outbox)
	srv := &http.Server{Addr: env.Addr, Handler: svc.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
