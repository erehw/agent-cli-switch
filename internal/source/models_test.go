package source

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "sk-test-SECRET-123"

// 故意带奇怪空白、非 ASCII、无结尾换行，验证逐字节透传。
var rawModels = []byte("{\"object\":\"list\",\n  \"data\":[{\"id\":\"m-1\"},\t{\"id\":\"模型2\"}]  }")

func TestModelsURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.example/v1":   "https://x.example/v1/models",
		"https://x.example/v1/":  "https://x.example/v1/models",
		"https://x.example/v1//": "https://x.example/v1/models",
		" https://x.example/v1 ": "https://x.example/v1/models",
	} {
		if got := ModelsURL(in); got != want {
			t.Fatal(in, got)
		}
	}
}

func TestFetchModelsPassthrough(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(rawModels)
	}))
	defer srv.Close()
	for _, base := range []string{srv.URL + "/v1", srv.URL + "/v1/"} {
		body, err := FetchModels(context.Background(), srv.Client(), base, testKey)
		if err != nil || !bytes.Equal(body, rawModels) {
			t.Fatal(base, string(body), err)
		}
		if gotAuth != "Bearer "+testKey || gotPath != "/v1/models" {
			t.Fatal(gotAuth == "", gotPath)
		}
	}
}

func TestFetchModelsErrorsHideKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		// 恶意/粗心的服务端把请求头回显出来
		_, _ = w.Write([]byte(`{"error":"bad key","echo":"` + r.Header.Get("Authorization") + `"}` + strings.Repeat("x", 2000)))
	}))
	defer srv.Close()
	_, err := FetchModels(context.Background(), srv.Client(), srv.URL+"/v1", testKey)
	var he *ModelsHTTPError
	if !errors.As(err, &he) || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad key") {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), testKey) || len(he.Body) > 600 {
		t.Fatal("错误里不能有 key，且要截断", len(he.Body))
	}
	srv.Close()
	_, err = FetchModels(context.Background(), http.DefaultClient, srv.URL+"/v1", testKey)
	if err == nil || strings.Contains(err.Error(), testKey) {
		t.Fatal(err)
	}
}
