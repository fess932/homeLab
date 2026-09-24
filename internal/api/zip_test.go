package api

import (
	"archive/zip"
	"bytes"
)

func newZip(buf *bytes.Buffer) func(name string, data []byte) {
	zw := zip.NewWriter(buf)
	return func(name string, data []byte) {
		w, _ := zw.Create(name)
		_, _ = w.Write(data)
		_ = zw.Close()
	}
}
