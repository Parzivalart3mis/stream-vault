package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/yashk/streamvault/internal/api"
	"github.com/yashk/streamvault/internal/leaderboard"
	"github.com/yashk/streamvault/internal/processor"
	"github.com/yashk/streamvault/internal/store"
)

// sharedSrv is the single test server shared across all tests in this package.
var sharedSrv *httptest.Server

func TestMain(m *testing.M) {
	// Ryuk (the testcontainers reaper) struggles with Docker Desktop's socket path on Linux.
	// Disabling it is safe for tests — containers are cleaned up by Terminate() in the cleanup path.
	os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")

	ctx, cancelCtx := context.WithCancel(context.Background())

	_, file, _, _ := runtime.Caller(0)
	migrationsPath := filepath.Join(filepath.Dir(file), "..", "internal", "store", "migrations")

	pgc, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("streamvault_test"),
		tcpostgres.WithUsername("streamvault"),
		tcpostgres.WithPassword("streamvault"),
		tcpostgres.WithInitScripts(filepath.Join(migrationsPath, "001_init.up.sql")),
		tcpostgres.WithSQLDriver("pgx"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres container: %v\n", err)
		os.Exit(1)
	}

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get connection string: %v\n", err)
		os.Exit(1)
	}

	db, err := store.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to db: %v\n", err)
		os.Exit(1)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	lb := leaderboard.New(db, log)
	lb.Start(ctx, time.Hour)

	pipeline := processor.New(db, lb, 4, 500, log)
	pipeline.Start(ctx)

	sharedSrv = httptest.NewServer(api.NewRouter(db, pipeline, lb, log))

	code := m.Run()

	sharedSrv.Close()
	cancelCtx() // unblocks leaderboard + pipeline goroutines
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = pipeline.Shutdown(shutCtx)
	lb.Shutdown()
	db.Close()
	_ = pgc.Terminate(shutCtx)

	os.Exit(code)
}

// post fires a JSON POST and returns status + decoded envelope.
func post(t *testing.T, path string, body any, viewerID string) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, sharedSrv.URL+path, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if viewerID != "" {
		req.Header.Set("X-Viewer-Id", viewerID)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var env map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	return resp.StatusCode, env
}

func get(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(sharedSrv.URL + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	var env map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	return resp.StatusCode, env
}

func dataID(env map[string]any) string {
	data, _ := env["data"].(map[string]any)
	id, _ := data["id"].(string)
	return id
}

func newCreator(t *testing.T, name string) string {
	t.Helper()
	_, env := post(t, "/creators", map[string]string{"display_name": name}, "")
	id := dataID(env)
	require.NotEmpty(t, id)
	return id
}

// --- Tests ---

func TestHealth(t *testing.T) {
	status, env := get(t, "/health")
	assert.Equal(t, http.StatusOK, status)
	data, _ := env["data"].(map[string]any)
	assert.Equal(t, "ok", data["status"])
	assert.Equal(t, "ok", data["db"])
}

func TestCreateCreator(t *testing.T) {
	status, env := post(t, "/creators", map[string]string{"display_name": "ninja"}, "")
	assert.Equal(t, http.StatusCreated, status)
	assert.NotEmpty(t, dataID(env))
	assert.Nil(t, env["error"])
}

func TestCreateCreator_Validation(t *testing.T) {
	status, env := post(t, "/creators", map[string]string{}, "")
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.NotNil(t, env["error"])
}

func TestSubscribe_AllTiers(t *testing.T) {
	id := newCreator(t, "shroud")
	for _, tier := range []int{1, 2, 3} {
		status, resp := post(t, "/creators/"+id+"/subscribe", map[string]int{"tier": tier}, "viewer-1")
		assert.Equal(t, http.StatusAccepted, status, "tier %d", tier)
		assert.Nil(t, resp["error"])
	}
}

func TestSubscribe_MissingViewerID(t *testing.T) {
	id := newCreator(t, "pokimane")
	status, resp := post(t, "/creators/"+id+"/subscribe", map[string]int{"tier": 1}, "")
	assert.Equal(t, http.StatusBadRequest, status)
	errObj, _ := resp["error"].(map[string]any)
	assert.Equal(t, "MISSING_VIEWER_ID", errObj["code"])
}

func TestSubscribe_InvalidTier(t *testing.T) {
	id := newCreator(t, "xqc")
	status, _ := post(t, "/creators/"+id+"/subscribe", map[string]int{"tier": 9}, "viewer-1")
	assert.Equal(t, http.StatusUnprocessableEntity, status)
}

func TestGift(t *testing.T) {
	id := newCreator(t, "ludwig")
	status, resp := post(t, "/creators/"+id+"/gift", map[string]int{"quantity": 10}, "viewer-2")
	assert.Equal(t, http.StatusAccepted, status)
	assert.Nil(t, resp["error"])
}

func TestTip(t *testing.T) {
	id := newCreator(t, "mizkif")
	cases := []struct {
		cents  int
		status int
	}{
		{500, http.StatusAccepted},
		{100000, http.StatusAccepted},
		{0, http.StatusUnprocessableEntity},
		{100001, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		status, _ := post(t, "/creators/"+id+"/tip", map[string]int{"amount_cents": tc.cents}, "viewer-3")
		assert.Equal(t, tc.status, status, "amount_cents=%d", tc.cents)
	}
}

func TestCreatorNotFound(t *testing.T) {
	status, _ := post(t, "/creators/00000000-0000-0000-0000-000000000000/subscribe",
		map[string]int{"tier": 1}, "viewer-1")
	assert.Equal(t, http.StatusNotFound, status)
}

func TestRevenue_GroupByType(t *testing.T) {
	id := newCreator(t, "summit1g")
	post(t, "/creators/"+id+"/subscribe", map[string]int{"tier": 1}, "v1")
	post(t, "/creators/"+id+"/tip", map[string]int{"amount_cents": 500}, "v2")
	time.Sleep(200 * time.Millisecond)

	status, resp := get(t, "/creators/"+id+"/revenue?group_by=type")
	assert.Equal(t, http.StatusOK, status)
	data, _ := resp["data"].([]any)
	assert.GreaterOrEqual(t, len(data), 2)
}

func TestEvents_Pagination(t *testing.T) {
	id := newCreator(t, "timthetatman")
	for i := 0; i < 5; i++ {
		post(t, "/creators/"+id+"/tip",
			map[string]int{"amount_cents": (i + 1) * 100}, fmt.Sprintf("viewer-%d", i))
	}
	time.Sleep(200 * time.Millisecond)

	status, resp := get(t, "/creators/"+id+"/events?limit=3")
	assert.Equal(t, http.StatusOK, status)
	data, _ := resp["data"].(map[string]any)
	events, _ := data["events"].([]any)
	assert.Len(t, events, 3)

	nextCursor, _ := data["next_cursor"].(string)
	assert.NotEmpty(t, nextCursor)

	status2, resp2 := get(t, "/creators/"+id+"/events?limit=3&cursor="+nextCursor)
	assert.Equal(t, http.StatusOK, status2)
	data2, _ := resp2["data"].(map[string]any)
	events2, _ := data2["events"].([]any)
	assert.Len(t, events2, 2)
}

func TestLeaderboard(t *testing.T) {
	id := newCreator(t, "amouranth")
	post(t, "/creators/"+id+"/tip", map[string]int{"amount_cents": 5000}, "v1")
	time.Sleep(200 * time.Millisecond)

	status, resp := get(t, "/leaderboard")
	assert.Equal(t, http.StatusOK, status)
	data, _ := resp["data"].(map[string]any)
	entries, _ := data["entries"].([]any)
	assert.NotEmpty(t, entries)

	// Verify our creator appears in the leaderboard
	found := false
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		if entry["creator_id"] == id {
			found = true
			break
		}
	}
	assert.True(t, found, "creator %s should be in leaderboard", id)
}
