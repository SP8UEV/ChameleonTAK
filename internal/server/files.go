package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"chameleontak/internal/store"
)

const maxUpload = 512 << 20

var reHash = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func (a *App) filePath(hash string) string { return a.DataPath("files", strings.ToLower(hash)) }

// saveContent streams r to disk, returning its SHA-256 and size.
func (a *App) saveContent(r io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(a.DataPath("files"), "upload-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, maxUpload+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	if n > maxUpload {
		return "", 0, invalid("Plik jest za duży (maks. 512 MB).")
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err := os.Rename(tmp.Name(), a.filePath(sum)); err != nil {
		return "", 0, err
	}
	return sum, n, nil
}

func guessMIME(name, fallback string) string {
	if fallback != "" && fallback != "application/octet-stream" {
		return fallback
	}
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		return t
	}
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		return "application/x-zip-compressed"
	}
	return "application/octet-stream"
}

func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." {
		name = "plik"
	}
	return name
}

func fileJSON(f *store.File) map[string]any {
	kw := f.Keywords
	if kw == nil {
		kw = []string{}
	}
	return map[string]any{
		"hash": f.Hash, "uid": f.UID, "name": f.Name, "mime": f.MIME, "size": f.Size, "keywords": kw,
		"creatorUid": f.CreatorUID, "submitter": f.Submitter, "public": f.Tool != "private", "createdAt": f.CreatedAt,
	}
}

func (a *App) serveStoredFile(w http.ResponseWriter, r *http.Request, f *store.File) {
	fh, err := os.Open(a.filePath(f.Hash))
	if err != nil {
		httpError(w, http.StatusNotFound, "Plik nie istnieje na dysku.")
		return
	}
	defer fh.Close()
	w.Header().Set("Content-Type", f.MIME)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, cleanName(f.Name)))
	http.ServeContent(w, r, "", f.CreatedAt, fh)
}

func (a *App) deleteStoredFile(hash string) error {
	if err := a.store.DeleteFile(hash); err != nil {
		return err
	}
	if err := os.Remove(a.filePath(hash)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ---------- panel ----------

func (a *App) hListFiles(w http.ResponseWriter, r *http.Request) {
	fs, err := a.store.ListFiles()
	if err != nil {
		fail(w, err)
		return
	}
	out := []map[string]any{}
	for _, f := range fs {
		out = append(out, fileJSON(f))
	}
	writeJSON(w, 200, out)
}

func (a *App) hUploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	f, hdr, err := r.FormFile("file")
	if err != nil {
		fail(w, invalid("Wybierz plik do wysłania."))
		return
	}
	defer f.Close()
	hash, size, err := a.saveContent(f)
	if err != nil {
		fail(w, err)
		return
	}
	me := r.Context().Value(userKey).(*store.User)
	name := cleanName(hdr.Filename)
	kw := []string{}
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		kw = append(kw, "missionpackage")
	}
	rec := &store.File{
		Hash: hash, UID: randomUUID(), Name: name, MIME: guessMIME(name, hdr.Header.Get("Content-Type")), Size: size,
		Keywords: kw, CreatorUID: a.serverUID(), Submitter: me.Username, Tool: "public",
	}
	if err := a.store.UpsertFile(rec); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, fileJSON(rec))
}

func (a *App) hUpdateFile(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if _, err := a.store.GetFile(hash); err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Public *bool `json:"public"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Public != nil {
		tool := "private"
		if *in.Public {
			tool = "public"
		}
		if err := a.store.SetFileTool(hash, tool); err != nil {
			fail(w, err)
			return
		}
	}
	f, _ := a.store.GetFile(hash)
	writeJSON(w, 200, fileJSON(f))
}

func (a *App) hDeleteFile(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if _, err := a.store.GetFile(hash); err != nil {
		fail(w, err)
		return
	}
	if err := a.deleteStoredFile(hash); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hDownloadFile(w http.ResponseWriter, r *http.Request) {
	f, err := a.store.GetFile(r.PathValue("hash"))
	if err != nil {
		fail(w, err)
		return
	}
	a.serveStoredFile(w, r, f)
}

func randomUUID() string {
	t := randomToken()
	return fmt.Sprintf("%s-%s-4%s-a%s-%s", t[0:8], t[8:12], t[13:16], t[17:20], t[20:32])
}
