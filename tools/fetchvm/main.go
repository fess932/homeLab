// fetchvm скачивает релиз VictoriaMetrics single-node для текущей ОС и архитектуры,
// сверяет sha256 с опубликованным checksums-файлом и кладёт бинарник по пути -out.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	version := flag.String("version", "", "версия VictoriaMetrics, например v1.152.0")
	out := flag.String("out", "", "куда положить бинарник")
	flag.Parse()
	if *version == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if _, err := os.Stat(*out); err == nil {
		return
	}
	if err := fetch(*version, *out); err != nil {
		fmt.Fprintln(os.Stderr, "fetchvm:", err)
		os.Exit(1)
	}
}

func fetch(version, out string) error {
	name := fmt.Sprintf("victoria-metrics-%s-%s-%s", runtime.GOOS, runtime.GOARCH, version)
	archive, member := name+".tar.gz", "victoria-metrics-prod"
	if runtime.GOOS == "windows" {
		archive, member = name+".zip", fmt.Sprintf("victoria-metrics-%s-%s-prod.exe", runtime.GOOS, runtime.GOARCH)
	}
	base := "https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/" + version + "/"

	sums, err := download(base + name + "_checksums.txt")
	if err != nil {
		return err
	}
	want, err := checksum(sums, archive)
	if err != nil {
		return err
	}
	data, err := download(base + archive)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum %s не совпадает", archive)
	}

	var bin []byte
	if strings.HasSuffix(archive, ".zip") {
		bin, err = fromZip(data, member)
	} else {
		bin, err = fromTarGz(data, member)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}

func download(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func checksum(sums []byte, file string) (string, error) {
	for line := range strings.Lines(string(sums)) {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == file {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("в checksums нет %s", file)
}

func fromTarGz(data []byte, member string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("в архиве нет %s", member)
		}
		if err != nil {
			return nil, err
		}
		if h.Name == member {
			return io.ReadAll(tr)
		}
	}
}

func fromZip(data []byte, member string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	f, err := zr.Open(member)
	if err != nil {
		return nil, fmt.Errorf("в архиве нет %s: %w", member, err)
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
