package maukirim

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
)

// parseMultipart parses the recorded request body as multipart/form-data.
func (rr *requestRecorder) parseMultipart(maxMemory int64) error {
	snap := rr.snapshot()
	_, params, err := mime.ParseMediaType(snap.header.Get("Content-Type"))
	if err != nil {
		return err
	}
	form, err := multipart.NewReader(bytes.NewReader(snap.body), params["boundary"]).ReadForm(maxMemory)
	if err != nil {
		return err
	}
	rr.mu.Lock()
	rr.form = form
	rr.mu.Unlock()
	return nil
}

func (rr *requestRecorder) fileHeader(field string) *multipart.FileHeader {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	if rr.form == nil || len(rr.form.File[field]) == 0 {
		return nil
	}
	return rr.form.File[field][0]
}

func (rr *requestRecorder) fileName(field string) string {
	if fh := rr.fileHeader(field); fh != nil {
		return fh.Filename
	}
	return ""
}

func (rr *requestRecorder) fileContentType(field string) string {
	if fh := rr.fileHeader(field); fh != nil {
		return fh.Header.Get("Content-Type")
	}
	return ""
}

func (rr *requestRecorder) fileBody(field string) string {
	fh := rr.fileHeader(field)
	if fh == nil {
		return ""
	}
	f, err := fh.Open()
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	return string(b)
}

func (rr *requestRecorder) formValue(field string) string {
	snap := rr.snapshot()
	if snap.form == nil {
		return ""
	}
	values := snap.form.Value[field]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
