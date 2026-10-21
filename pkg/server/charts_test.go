package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestAccessChart_ServedAsAHelmRepositoryWithoutAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestAPIServer(t, redisStore())

	rr := doRequestWithHeaders(t, srv, http.MethodGet, "/charts/index.yaml", nil, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var index struct {
		Entries map[string][]struct {
			Name, Version, Digest string
			URLs                  []string `json:"urls"`
		} `json:"entries"`
	}
	require.NoError(t, yaml.Unmarshal(rr.Body.Bytes(), &index))
	entries := index.Entries[accessChartName]
	require.Len(t, entries, 1)
	require.Equal(t, []string{accessChartFile}, entries[0].URLs)

	rr = doRequestWithHeaders(t, srv, http.MethodGet, "/charts/"+entries[0].URLs[0], nil, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	digest := sha256.Sum256(rr.Body.Bytes())
	assert.Equal(t, hex.EncodeToString(digest[:]), entries[0].Digest)

	gz, err := gzip.NewReader(bytes.NewReader(rr.Body.Bytes()))
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Equal(t, []string{accessChartName + "/Chart.yaml"}, names, "a chart with no templates deploys nothing")
}
