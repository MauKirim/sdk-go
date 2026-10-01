package maukirim

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// multipartField is a plain form field in a multipart body.
type multipartField struct {
	name  string
	value string
}

// uploadFile is a file part in a multipart body.
type uploadFile struct {
	field       string
	fileName    string
	contentType string
	reader      io.Reader
}

// buildMultipart renders fields and files as multipart/form-data, returning the
// buffered body and the matching Content-Type header value. The body is
// buffered so a retry can replay the exact same bytes.
func buildMultipart(fields []multipartField, files ...uploadFile) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	for _, f := range files {
		if f.reader == nil {
			return nil, "", fmt.Errorf("maukirim: file part %q has no reader", f.field)
		}
	}

	for _, f := range fields {
		if err := w.WriteField(f.name, f.value); err != nil {
			return nil, "", err
		}
	}

	for _, f := range files {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
			escapeQuotes(f.field), escapeQuotes(f.fileName)))
		contentType := f.contentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header.Set("Content-Type", contentType)

		part, err := w.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err := io.Copy(part, f.reader); err != nil {
			return nil, "", err
		}
	}

	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, `\"`)

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }
