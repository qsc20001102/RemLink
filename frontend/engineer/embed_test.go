package engineerui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	wailsassetserver "github.com/wailsapp/wails/v2/pkg/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func TestEmbeddedProductionAssetsAreServedByWails(t *testing.T) {
	handler, err := wailsassetserver.NewAssetHandler(assetserver.Options{Assets: Assets}, nil)
	if err != nil {
		t.Fatalf("create Wails asset handler: %v", err)
	}

	index := getEmbeddedAsset(t, handler, "/", "text/html")
	references := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(string(index), -1)
	if len(references) < 2 {
		t.Fatalf("expected JavaScript and CSS references in index.html, got %q", index)
	}
	for _, reference := range references {
		getEmbeddedAsset(t, handler, reference[1], "")
	}
}

func getEmbeddedAsset(t *testing.T, handler http.Handler, path string, expectedContentType string) []byte {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://wails.localhost"+path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s returned %d: %s", path, response.Code, response.Body.String())
	}
	if expectedContentType != "" && response.Header().Get("Content-Type") != expectedContentType+"; charset=utf-8" {
		t.Fatalf("GET %s returned Content-Type %q", path, response.Header().Get("Content-Type"))
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read GET %s response: %v", path, err)
	}
	if len(body) == 0 {
		t.Fatalf("GET %s returned an empty body", path)
	}
	return body
}
