package claudeupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRejectInvalidHomeBeforeIO(t *testing.T) {
	for _, home := range []string{"", " ", ".", "relative/home"} {
		t.Run(home, func(t *testing.T) {
			u := New(Defaults(home, ""), nil)
			u.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid home reached network"); return nil, nil })
			if _, err := u.Run(context.Background(), "latest"); err == nil || !strings.Contains(err.Error(), "绝对路径") {
				t.Fatalf("Run: %v", err)
			}
			if err := u.install(context.Background(), "missing-installer", testVersion); err == nil || !strings.Contains(err.Error(), "绝对路径") {
				t.Fatalf("install: %v", err)
			}
			if got := Current(home); got != "" {
				t.Fatalf("Current: %q", got)
			}
		})
	}
}

func TestDownloadCompletePartAvoidsNetwork(t *testing.T) {
	for _, status := range []int{0, http.StatusRequestedRangeNotSatisfiable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			part := filepath.Join(t.TempDir(), "complete.part")
			data := []byte("already complete verified binary")
			if err := os.WriteFile(part, data, 0600); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			u := New(Defaults(t.TempDir(), ""), nil)
			u.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("complete part should be verified before requesting network")
				if status == 0 {
					return nil, errors.New("network unavailable")
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})
			if err := u.download(context.Background(), "https://example.invalid/bin", part, hex.EncodeToString(sum[:])); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(part)
			if err != nil || string(got) != string(data) {
				t.Fatalf("part: %q %v", got, err)
			}
		})
	}
}
