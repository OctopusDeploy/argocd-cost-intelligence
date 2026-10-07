package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAppRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    AppRef
		wantErr bool
	}{
		{in: "argocd:redis", want: AppRef{Namespace: "argocd", Name: "redis"}},
		{in: "team-a:web.frontend", want: AppRef{Namespace: "team-a", Name: "web.frontend"}},
		{in: "", wantErr: true},
		{in: "redis", wantErr: true},
		{in: ":redis", wantErr: true},
		{in: "argocd:", wantErr: true},
		{in: "argocd:redis:extra", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAppRef(tt.in)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.in, got.String())
		})
	}
}
