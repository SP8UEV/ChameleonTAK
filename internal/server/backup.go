package server

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	restoreDir    = "restore-staging"
	resetMarker   = ".factory-reset"
	backupDBName  = "chameleontak.db"
	maxRestoreLen = 4 << 30
)

// RestartFunc is called when the process must restart (systemd brings it back).
var RestartFunc = func() { os.Exit(0) }

func (a *App) scheduleRestart(reboot bool) {
	go func() {
		time.Sleep(time.Second)
		a.pos.Flush()
		a.stopServices()
		_ = a.store.Close()
		if reboot && a.helper.Available() {
			if err := a.helper.Run("", "reboot"); err == nil {
				return
			}
		}
		RestartFunc()
	}()
}

func (a *App) hBackup(w http.ResponseWriter, r *http.Request) {
	snap := a.DataPath("backup-snapshot.db")
	_ = os.Remove(snap)
	if _, err := a.store.DB().Exec(`VACUUM INTO ?`, snap); err != nil {
		fail(w, fmt.Errorf("kopia bazy: %w", err))
		return
	}
	defer os.Remove(snap)

	name := fmt.Sprintf("chameleontak-backup-%s.tar.gz", time.Now().Format("20060102-1504"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	addFile := func(src, name string) error {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	}
	err := addFile(snap, backupDBName)
	for _, dir := range []string{"pki", "files"} {
		if err != nil {
			break
		}
		err = filepath.WalkDir(a.DataPath(dir), func(p string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			rel, _ := filepath.Rel(a.opts.DataDir, p)
			return addFile(p, filepath.ToSlash(rel))
		})
	}
	if err != nil {
		a.log.Error("kopia zapasowa", "err", err)
	}
	_ = tw.Close()
	_ = gz.Close()
}

func (a *App) hRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRestoreLen)
	f, _, err := r.FormFile("file")
	if err != nil {
		fail(w, invalid("Wybierz plik kopii zapasowej (.tar.gz)."))
		return
	}
	defer f.Close()
	staging := a.DataPath(restoreDir)
	_ = os.RemoveAll(staging)
	if err := extractBackup(f, staging); err != nil {
		_ = os.RemoveAll(staging)
		fail(w, invalid("Niepoprawna kopia zapasowa: "+err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Kopia wgrana. Serwer uruchomi się ponownie za chwilę."})
	a.scheduleRestart(false)
}

func extractBackup(src io.Reader, dst string) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	hasDB := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Clean(filepath.FromSlash(h.Name))
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("niedozwolona ścieżka %q", h.Name)
		}
		top := strings.Split(filepath.ToSlash(name), "/")[0]
		if name != backupDBName && top != "pki" && top != "files" {
			continue
		}
		if name == backupDBName {
			hasDB = true
		}
		out := filepath.Join(dst, name)
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return err
		}
		fw, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, tr)
		fw.Close()
		if err != nil {
			return err
		}
	}
	if !hasDB {
		return errors.New("brak bazy danych w archiwum")
	}
	return nil
}

func (a *App) hFactoryReset(w http.ResponseWriter, r *http.Request) {
	var in struct{ Confirm string }
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Confirm != "RESET" {
		fail(w, invalid("Aby potwierdzić, wpisz RESET."))
		return
	}
	if err := os.WriteFile(a.DataPath(resetMarker), []byte("1"), 0o600); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Przywracanie ustawień fabrycznych — urządzenie uruchomi się ponownie."})
	if a.helper.Available() {
		// Drop the field hotspot so the Pi comes back up with the setup hotspot / captive wizard.
		go func() {
			if err := a.helper.Run("", "hotspot-off"); err != nil {
				a.log.Warn("hotspot-off", "err", err)
			}
		}()
	}
	a.scheduleRestart(true)
}

// PrepareDataDir applies a pending restore or factory reset before the database is opened.
func PrepareDataDir(dataDir string) error {
	if _, err := os.Stat(filepath.Join(dataDir, resetMarker)); err == nil {
		return wipeDataDir(dataDir)
	}
	staging := filepath.Join(dataDir, restoreDir)
	if _, err := os.Stat(filepath.Join(staging, backupDBName)); err != nil {
		return nil
	}
	for _, n := range []string{backupDBName, backupDBName + "-wal", backupDBName + "-shm"} {
		_ = os.Remove(filepath.Join(dataDir, n))
	}
	for _, d := range []string{"pki", "files"} {
		_ = os.RemoveAll(filepath.Join(dataDir, d))
		if _, err := os.Stat(filepath.Join(staging, d)); err == nil {
			if err := os.Rename(filepath.Join(staging, d), filepath.Join(dataDir, d)); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(filepath.Join(staging, backupDBName), filepath.Join(dataDir, backupDBName)); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(dataDir, ".setup-done"), []byte("restored\n"), 0o644)
	return os.RemoveAll(staging)
}

// wipeDataDir removes everything the server created (the directory itself stays).
func wipeDataDir(dataDir string) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dataDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// FactoryReset wipes the data directory immediately (CLI).
func FactoryReset(dataDir string) error { return wipeDataDir(dataDir) }
