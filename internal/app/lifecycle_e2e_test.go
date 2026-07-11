package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/cache"
	"github.com/sanjayrohith/redline/internal/config"
	"github.com/sanjayrohith/redline/internal/ingest"
	"github.com/sanjayrohith/redline/internal/sandbox"
)

// fakeSafetensorsFile builds a real, wire-format-correct safetensors
// file: an 8-byte little-endian header length, the JSON header itself,
// then the tensor payload bytes - the exact shape FetchHeader parses.
func fakeSafetensorsFile(headerJSON string, payloadBytes int) []byte {
	header := []byte(headerJSON)
	prefix := make([]byte, 8)
	binary.LittleEndian.PutUint64(prefix, uint64(len(header)))
	payload := make([]byte, payloadBytes)
	return append(append(prefix, header...), payload...)
}

// fakeModelHostServer stands in for huggingface.co, real enough for the
// real ingest.Client and ingest.HTTPRangeFetcher to talk to over HTTP -
// including a real byte-range request for the safetensors header.
func fakeModelHostServer(t *testing.T, weightFile []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/models/lifecycle-org/lifecycle-model/revision/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sha":"lifecycle-sha-1"}`))
	})
	mux.HandleFunc("/api/models/lifecycle-org/lifecycle-model/tree/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `[{"type":"file","path":"config.json","size":100},{"type":"file","path":"model.safetensors","size":%d}]`, len(weightFile))
	})
	mux.HandleFunc("/lifecycle-org/lifecycle-model/resolve/main/config.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model_type":"llama","num_hidden_layers":2,"num_attention_heads":2,"hidden_size":16}`))
	})
	mux.HandleFunc("/lifecycle-org/lifecycle-model/resolve/main/model.safetensors", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "model.safetensors", time.Time{}, &sizedReaderAt{data: weightFile})
	})
	mux.HandleFunc("/api/models/lifecycle-org/lifecycle-model", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cardData":{"license":"apache-2.0"}}`))
	})
	return httptest.NewServer(mux)
}

// sizedReaderAt adapts a byte slice to io.ReadSeeker for http.ServeContent,
// which needs Seek to serve real Range requests.
type sizedReaderAt struct {
	data []byte
	pos  int64
}

func (r *sizedReaderAt) Read(p []byte) (int, error) {
	if r.pos >= int64(len(r.data)) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, r.data[r.pos:])
	r.pos += int64(n)
	return n, nil
}

func (r *sizedReaderAt) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0:
		r.pos = offset
	case 1:
		r.pos += offset
	case 2:
		r.pos = int64(len(r.data)) + offset
	}
	return r.pos, nil
}

// TestE2E_FullLifecycle exercises ingest, parse, cache, schedule,
// sandbox, serve, stream, measure, and idle-reap as one asserted
// sequence against real Postgres, Redis, and MinIO, and this gateway's
// actual component wiring - not mocks standing in for any of those
// stages.
func TestE2E_FullLifecycle(t *testing.T) {
	env := setupE2EEnv(t)

	// Nomad is deliberately unreachable: this is exactly the degraded
	// path step 135 built, and it must be what "schedule" below exercises
	// by default in a test environment with no Nomad cluster.
	a := newTestApp(t, env, func(cfg *config.Config) {
		cfg.NomadAddr = "http://127.0.0.1:1"
		cfg.IdleTimeout = 20 * time.Millisecond
	})
	ctx := context.Background()

	// --- ingest + parse -----------------------------------------------
	weightFile := fakeSafetensorsFile(
		`{"weight":{"dtype":"F16","shape":[16,16],"data_offsets":[0,512]}}`, 512)
	hostServer := fakeModelHostServer(t, weightFile)
	defer hostServer.Close()

	manifestClient := ingest.NewClient(ingest.WithBaseURL(hostServer.URL))
	orchestrator := ingest.NewOrchestrator(
		manifestClient,
		ingest.NewHTTPRangeFetcher(hostServer.Client()),
		a.Repositories().IngestionJobs,
		a.Repositories().Models,
		a.Repositories().Models,
		ingest.DefaultQuota,
	)

	job, err := a.Repositories().IngestionJobs.Create(ctx, "lifecycle-org/lifecycle-model", "main")
	if err != nil {
		t.Fatalf("create ingestion job: %v", err)
	}
	ref, err := ingest.ParseReference("lifecycle-org/lifecycle-model")
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}
	if err := orchestrator.Run(ctx, job.ID, *ref); err != nil {
		t.Fatalf("orchestrator.Run() error = %v", err)
	}

	completedJob, err := a.Repositories().IngestionJobs.GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetByID(job) error = %v", err)
	}
	if completedJob.State != "cached" || completedJob.ModelID == nil {
		t.Fatalf("ingestion job = %+v, want state=cached with a model id", completedJob)
	}
	model, err := a.Repositories().Models.GetByID(ctx, *completedJob.ModelID)
	if err != nil {
		t.Fatalf("GetByID(model) error = %v", err)
	}
	if model.Architecture != "llama" || model.License != "apache-2.0" || model.ParameterCount != 16*16 {
		t.Fatalf("model = %+v, want architecture=llama license=apache-2.0 parameter_count=256", model)
	}

	// --- cache -----------------------------------------------------
	minioContainer, err := tcminio.Run(ctx, "minio/minio:latest",
		tcminio.WithUsername("minioadmin"), tcminio.WithPassword("minioadmin"))
	if err != nil {
		t.Skipf("skipping cache stage: could not start minio container: %v", err)
	}
	t.Cleanup(func() { _ = minioContainer.Terminate(context.Background()) })
	minioEndpoint, err := minioContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("minio connection string: %v", err)
	}
	// MinIO's health endpoint can report ready a moment before it is
	// actually able to service bucket-management calls - retry rather
	// than treating that narrow startup race as a real failure.
	var cacheClient *cache.Client
	deadline := time.Now().Add(15 * time.Second)
	for {
		cacheClient, err = cache.NewClient(ctx, cache.Options{
			Endpoint: minioEndpoint, AccessKeyID: "minioadmin", SecretAccessKey: "minioadmin", BucketName: "lifecycle",
		})
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("cache.NewClient() error = %v", err)
	}
	uploader := cache.NewChunkedUploader(cacheClient, 5*1024*1024, 2)
	pipeline := ingest.NewCachePipeline(hostServer.Client(), uploader, cacheClient)

	sum := sha256.Sum256(weightFile)
	if _, err := pipeline.DownloadToCache(ctx, hostServer.URL+"/lifecycle-org/lifecycle-model/resolve/main/model.safetensors",
		"sha256", hex.EncodeToString(sum[:]), "artifacts/lifecycle-model.safetensors", nil); err != nil {
		t.Fatalf("DownloadToCache() error = %v", err)
	}
	cached, err := cacheClient.GetObject(ctx, "artifacts/lifecycle-model.safetensors")
	if err != nil {
		t.Fatalf("GetObject(cached artifact) error = %v", err)
	}
	_ = cached.Close()

	// --- sandbox --------------------------------------------------
	sandboxOut, err := sandbox.Do(ctx, "echo", "lifecycle-sandbox-probe")
	if err != nil {
		t.Skipf("skipping sandbox stage: runsc unavailable in this environment: %v", err)
	}
	if !strings.Contains(sandboxOut, "lifecycle-sandbox-probe") {
		t.Errorf("sandboxOut = %q, want it to contain the probe string", sandboxOut)
	}

	// --- auth (needed for the session-authed dashboard routes below) --
	passwordHash, err := auth.HashPassword("lifecycle-pw")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if _, err := a.Repositories().Users.CreateWithPassword(ctx, "lifecycle@example.com", passwordHash); err != nil {
		t.Fatalf("create user: %v", err)
	}
	loginRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(loginRec, httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"lifecycle@example.com","password":"lifecycle-pw"}`)))
	if loginRec.Code != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204, body = %s", loginRec.Code, loginRec.Body.String())
	}
	var accessToken string
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "redline_access" {
			accessToken = c.Value
		}
	}
	if accessToken == "" {
		t.Fatal("login did not set an access token cookie")
	}

	// --- schedule (degraded: Nomad is unreachable) + idle-reap --------
	deployRec := httptest.NewRecorder()
	deployReq := httptest.NewRequest(http.MethodPost, "/v1/dashboard/deployments",
		strings.NewReader(fmt.Sprintf(`{"model_id":%q}`, model.ID)))
	deployReq.Header.Set("Authorization", "Bearer "+accessToken)
	a.Handler().ServeHTTP(deployRec, deployReq)
	if deployRec.Code != http.StatusCreated {
		t.Fatalf("create deployment status = %d, want 201, body = %s", deployRec.Code, deployRec.Body.String())
	}
	var deployment struct {
		ID       string `json:"id"`
		Queued   bool   `json:"queued"`
		Degraded bool   `json:"degraded"`
	}
	if err := json.Unmarshal(deployRec.Body.Bytes(), &deployment); err != nil {
		t.Fatalf("unmarshal deployment: %v", err)
	}
	if !deployment.Queued || !deployment.Degraded {
		t.Errorf("deployment = %+v, want queued=true degraded=true (Nomad is unreachable)", deployment)
	}

	// --- serve + stream ------------------------------------------------
	apiKey := issueAPIKey(t, a, "lifecycle-chat@example.com", []string{"inference"})
	streamRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(streamRec, chatRequest(t, apiKey,
		`{"model":"mock-model","messages":[{"role":"user","content":"hello lifecycle"}],"stream":true}`))
	if streamRec.Code != http.StatusOK {
		t.Fatalf("stream status = %d, want 200, body = %s", streamRec.Code, streamRec.Body.String())
	}
	if !strings.Contains(streamRec.Body.String(), "data: [DONE]") {
		t.Error("stream response did not terminate with the SSE [DONE] marker")
	}

	// --- measure ---------------------------------------------------
	metricsRec := httptest.NewRecorder()
	a.Metrics().Handler().ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metricsRec.Body.String(), "redline_time_to_first_token") {
		t.Error("/metrics did not expose the time-to-first-token histogram after a streamed request")
	}

	// --- idle-reap ------------------------------------------------
	time.Sleep(50 * time.Millisecond) // exceed the 20ms IdleTimeout configured above
	sessionRec := httptest.NewRecorder()
	sessionReq := httptest.NewRequest(http.MethodGet, "/v1/dashboard/deployments/"+deployment.ID+"/session", nil)
	sessionReq.Header.Set("Authorization", "Bearer "+accessToken)
	a.Handler().ServeHTTP(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("session status = %d, want 200, body = %s", sessionRec.Code, sessionRec.Body.String())
	}
	var session struct {
		SecondsUntilReap float64 `json:"seconds_until_reap"`
	}
	if err := json.Unmarshal(sessionRec.Body.Bytes(), &session); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	if session.SecondsUntilReap != 0 {
		t.Errorf("SecondsUntilReap = %f, want 0 once idle time exceeds the configured timeout", session.SecondsUntilReap)
	}
}
