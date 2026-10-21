package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// The access Application the Helm chart creates is sourced from a chart that renders nothing,
// served here as a Helm repository so Argo CD's repo server needs no other repository. Who may get
// that Application decides who sees cluster-wide cost data.
const (
	accessChartName    = "cost-intelligence-access"
	accessChartVersion = "1.0.0"
	accessChartFile    = accessChartName + "-" + accessChartVersion + ".tgz"

	accessChartDescription = "Deploys nothing. Who may get the Argo CD Application made from it may see cluster-wide costs in Cost Intelligence."
)

const accessChartYAML = `apiVersion: v2
name: ` + accessChartName + `
version: ` + accessChartVersion + `
description: ` + accessChartDescription + `
`

// accessChart is the packaged chart and the repository index that lists it.
var accessChart, accessChartIndex = packageAccessChart()

// packageAccessChart builds the chart archive and its index. The archive's timestamps are fixed, so
// every replica serves the same bytes and digest.
func packageAccessChart() (archive, index []byte) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	modTime := time.Unix(0, 0).UTC()
	files := []struct{ name, body string }{{accessChartName + "/Chart.yaml", accessChartYAML}}
	for _, f := range files {
		hdr := &tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body)), ModTime: modTime, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			panic(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			panic(err)
		}
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	if err := gz.Close(); err != nil {
		panic(err)
	}
	archive = buf.Bytes()

	digest := sha256.Sum256(archive)
	index = fmt.Appendf(nil, `apiVersion: v1
entries:
  %[1]s:
  - apiVersion: v2
    name: %[1]s
    version: %[2]s
    description: %[5]s
    digest: %[3]s
    created: "1970-01-01T00:00:00Z"
    urls:
    - %[4]s
generated: "1970-01-01T00:00:00Z"
`, accessChartName, accessChartVersion, hex.EncodeToString(digest[:]), accessChartFile, accessChartDescription)
	return archive, index
}

// GET /charts/index.yaml
func handleAccessChartIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-yaml")
	_, _ = w.Write(accessChartIndex)
}

// GET /charts/cost-intelligence-access-<version>.tgz
func handleAccessChart(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/gzip")
	_, _ = w.Write(accessChart)
}
