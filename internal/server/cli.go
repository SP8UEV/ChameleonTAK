package server

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// SetupCLI runs the first-time configuration without the web wizard; services are started by
// the next "serve".
func (a *App) SetupCLI(req SetupRequest) error {
	if a.cfg.Get().SetupDone {
		return errors.New("serwer jest już skonfigurowany (użyj factory-reset, aby zacząć od nowa)")
	}
	if err := a.validateSetup(&req); err != nil {
		return err
	}
	a.cliMode = true
	job := &setupJob{}
	for _, k := range []string{"time", "config", "ca", "server", "admin", "ssh", "firewall", "services", "network"} {
		job.steps = append(job.steps, setupStep{Key: k, State: "pending"})
	}
	err := a.RunSetup(req, job)
	for _, s := range job.steps {
		fmt.Printf("  %-9s %-8s %s\n", s.Key, s.State, s.Message)
	}
	return err
}

func (a *App) ResetAdmin(username, password string) error {
	if len(password) < 8 {
		return errors.New("hasło musi mieć co najmniej 8 znaków")
	}
	u, err := a.store.GetUserByName(username)
	if err != nil {
		return fmt.Errorf("użytkownik %q nie istnieje", username)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash, u.IsAdmin, u.Disabled = string(h), true, false
	if err := a.store.UpdateUser(u); err != nil {
		return err
	}
	return a.store.DeleteUserSessions(u.ID)
}
