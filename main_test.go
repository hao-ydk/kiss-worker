package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupTestServer(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	config.appKey = "test-secret"
	config.dataDir = t.TempDir()
	return newRouter()
}

func syncRequestForTest(t *testing.T, router http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/sync", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+calSha256(config.appKey, KV_SALT_SYNC))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func decodeSyncResponse(t *testing.T, res *httptest.ResponseRecorder) kvData {
	t.Helper()
	var data kvData
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, res.Body.String())
	}
	return data
}

func TestSyncAcceptsEmptyValueAndUsesMilliseconds(t *testing.T) {
	router := setupTestServer(t)
	res := syncRequestForTest(t, router, map[string]any{"key": "setting", "value": "", "updateAt": 0})
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	data := decodeSyncResponse(t, res)
	if data.Value != "" || data.UpdateAt < legacySecondsCutoff {
		t.Fatalf("unexpected response: %+v", data)
	}
}

func TestOpaqueKeyCannotEscapeDataDirectory(t *testing.T) {
	router := setupTestServer(t)
	outside := filepath.Join(filepath.Dir(config.dataDir), "escaped.json")
	res := syncRequestForTest(t, router, map[string]any{"key": "../escaped.json", "value": "safe", "updateAt": 1_700_000_000_000})
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("unexpected file outside data directory: %v", err)
	}
	if _, err := os.Stat(storagePath("../escaped.json")); err != nil {
		t.Fatalf("encoded storage file missing: %v", err)
	}
}

func TestLegacyFileIsReadAndMigratedOnWrite(t *testing.T) {
	router := setupTestServer(t)
	legacy := kvData{Key: "setting", Value: "old", UpdateAt: 1_700_000_000}
	payload, _ := json.Marshal(legacy)
	legacyPath := filepath.Join(config.dataDir, "setting")
	if err := os.WriteFile(legacyPath, payload, 0600); err != nil {
		t.Fatal(err)
	}

	res := syncRequestForTest(t, router, map[string]any{"key": "setting", "value": "local", "updateAt": 0})
	data := decodeSyncResponse(t, res)
	if data.Value != "old" || data.UpdateAt != 1_700_000_000_000 {
		t.Fatalf("legacy response mismatch: %+v", data)
	}

	res = syncRequestForTest(t, router, map[string]any{"key": "setting", "value": "new", "updateAt": 1_800_000_000_000})
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(storagePath("setting")); err != nil {
		t.Fatalf("encoded storage file missing: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy file should be retained: %v", err)
	}
}

func TestSyncRejectsMalformedRequests(t *testing.T) {
	router := setupTestServer(t)
	tests := []map[string]any{
		{"value": "x", "updateAt": 1},
		{"key": "", "value": "x", "updateAt": 1},
		{"key": "x", "value": "x", "updateAt": -1},
		{"key": string(bytes.Repeat([]byte("x"), maxKeyBytes+1)), "value": "x", "updateAt": 1},
	}
	for _, body := range tests {
		res := syncRequestForTest(t, router, body)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("body=%v status=%d response=%s", body, res.Code, res.Body.String())
		}
	}
}

func TestConcurrentSyncNeverRegresses(t *testing.T) {
	router := setupTestServer(t)
	const requests = 32
	const baseUpdateAt = int64(1_700_000_000_000)
	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for i := 1; i <= requests; i++ {
		wg.Add(1)
		go func(updateAt int64) {
			defer wg.Done()
			res := syncRequestForTest(t, router, map[string]any{
				"key": "rules", "value": fmt.Sprintf("value-%d", updateAt), "updateAt": updateAt,
			})
			if res.Code != http.StatusOK {
				errs <- fmt.Errorf("status=%d body=%s", res.Code, res.Body.String())
			}
		}(baseUpdateAt + int64(i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	res := syncRequestForTest(t, router, map[string]any{"key": "rules", "value": "local", "updateAt": 0})
	data := decodeSyncResponse(t, res)
	expectedUpdateAt := baseUpdateAt + requests
	if data.UpdateAt != expectedUpdateAt || data.Value != fmt.Sprintf("value-%d", expectedUpdateAt) {
		t.Fatalf("latest value was not retained: %+v", data)
	}
}

func TestCorruptStoredDataReturnsServerError(t *testing.T) {
	router := setupTestServer(t)
	if err := os.WriteFile(storagePath("setting"), []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	res := syncRequestForTest(t, router, map[string]any{"key": "setting", "value": "local", "updateAt": 1})
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestRulesKeepsExistingProtocol(t *testing.T) {
	router := setupTestServer(t)
	rules := `[{"pattern":"example.com"}]`
	keyLock := lockForKey(KV_RULES_SHARE_KEY)
	keyLock.Lock()
	err := saveDataUnlocked(&kvData{Key: KV_RULES_SHARE_KEY, Value: rules, UpdateAt: 1})
	keyLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	psk := calSha256(config.appKey, KV_SALT_SHARE)
	req := httptest.NewRequest(http.MethodGet, "/rules?psk="+psk, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != rules {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestLoadConfigRequiresAppKey(t *testing.T) {
	t.Setenv(ENV_APP_KEY, "")
	if err := loadConfig(); err == nil {
		t.Fatal("expected missing APP_KEY to fail")
	}
}

func TestCheckDirExistRejectsAFile(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkDirExist(filePath); err == nil {
		t.Fatal("expected a file data path to fail")
	}
}
