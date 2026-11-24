// Programa /stress: prueba de carga contra la API REST de Stamping.
//
// Genera actas electorales sinteticas con resultados aleatorios que cuadran,
// las sella por la API con la cuenta configurada y muestra el avance en vivo.
// Cada acta queda con un enlace al visor. Cada ejecucion deja un reporte
// (resumen y CSV) como evidencia de la prueba.
//
// ATENCION: sella registros reales, descuenta creditos de la cuenta y dispara
// anclajes en blockchain. No es una simulacion.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

const (
	maxCount       = 500000
	maxConcurrency = 200
	maxRows        = 100
)

// ───────────────────────────── configuracion ─────────────────────────────

type config struct {
	BaseURL     string
	ProcessCode string
	ProcessName string
	StationBase int
	Listen      string
	UIPassword  string
	VisorURL    string
	CustodyPath string
	CipherKey   string
	RunsDir     string
}

func (c *config) visorEnabled() bool {
	return c.VisorURL != "" && c.CustodyPath != "" && c.CipherKey != ""
}

func loadConfig() (*config, error) {
	path := os.Getenv("STRESS_ENV_FILE")
	explicit := path != ""
	if path == "" {
		path = ".env"
	}
	values := map[string]string{}
	file, err := os.Open(path)
	switch {
	case err == nil:
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
		file.Close()
	case os.IsNotExist(err) && !explicit:
		// Sin archivo: se admiten variables de entorno.
	default:
		return nil, fmt.Errorf("no se pudo leer el archivo de configuracion %s: %w", path, err)
	}

	get := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v := values[key]; v != "" {
			return v
		}
		return fallback
	}

	base, err := strconv.Atoi(get("STATION_BASE", "1000000"))
	if err != nil || base < 0 {
		return nil, fmt.Errorf("STATION_BASE debe ser un numero entero")
	}
	cfg := &config{
		BaseURL:     strings.TrimRight(get("STAMPING_BASE_URL", "https://api.stamping.io"), "/"),
		ProcessCode: get("PROCESS_CODE", "ZW-TEST"),
		ProcessName: get("PROCESS_NAME", "Prueba de estres Zenware"),
		StationBase: base,
		Listen:      get("LISTEN_ADDR", "127.0.0.1:8090"),
		UIPassword:  get("STRESS_UI_PASSWORD", ""),
		VisorURL:    strings.TrimRight(get("VISOR_URL", ""), "/"),
		CustodyPath: get("CUSTODY_PATH", ""),
		CipherKey:   get("CODE_CIPHER_KEY", ""),
		RunsDir:     get("RUNS_DIR", "./runs"),
	}
	if cfg.CipherKey != "" && len(cfg.CipherKey) != 64 {
		return nil, fmt.Errorf("CODE_CIPHER_KEY debe tener 64 caracteres hexadecimales")
	}
	if (cfg.VisorURL != "" || cfg.CustodyPath != "" || cfg.CipherKey != "") && !cfg.visorEnabled() {
		return nil, fmt.Errorf("para los enlaces al visor hacen falta VISOR_URL, CUSTODY_PATH y CODE_CIPHER_KEY juntas")
	}
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("LISTEN_ADDR invalida: %w", err)
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && host != "localhost" && cfg.UIPassword == "" {
		return nil, fmt.Errorf("si la interfaz escucha fuera de localhost, STRESS_UI_PASSWORD es obligatoria")
	}
	return cfg, nil
}

// ───────────────────────────── codigo del QR ─────────────────────────────

// encodeCode produce el mismo formato que el visor: nonce de 12 bytes mas
// AES-256-GCM, en base64url sin relleno.
func encodeCode(aead cipher.AEAD, identifier string) (string, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nil, nonce, []byte(identifier), nil)
	return base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func newAEAD(keyHex string) (cipher.AEAD, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("CODE_CIPHER_KEY invalida")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// ─────────────────────────────── actas ───────────────────────────────────

var parties = []struct{ Code, Name, Party string }{
	{"MRU", "Movimiento Regional Unidad", "M.R.U."},
	{"ACL", "Alianza Civica del Litoral", "ACL"},
	{"FVI", "Frente Vecinal Independiente", "Frente Vecinal"},
	{"PPL", "Partido del Progreso Local", "P.P.L."},
}

type acta struct {
	Station    string
	Identifier string
	Info       map[string]any
	Data       map[string]any
	Lines      []string
}

// newActa arma un acta que cumple los tres cuadres del contrato:
// suma de opciones = validos, validos + nulos + blancos = votantes,
// votantes <= electores habiles.
func newActa(cfg *config, runID string, n int) acta {
	station := strconv.Itoa(cfg.StationBase + n)
	eligible := 180 + mrand.IntN(121)
	floor := eligible * 60 / 100
	voters := floor + mrand.IntN(eligible-floor+1)
	null, blank := mrand.IntN(9), mrand.IntN(7)
	if null+blank > voters {
		null, blank = 0, 0
	}
	valid := voters - null - blank

	weights, total := make([]int, len(parties)), 0
	for i := range weights {
		weights[i] = 1 + mrand.IntN(100)
		total += weights[i]
	}
	votes, assigned := make([]int, len(parties)), 0
	for i := range votes {
		votes[i] = valid * weights[i] / total
		assigned += votes[i]
	}
	votes[0] += valid - assigned

	options := make([]map[string]any, len(parties))
	lines := []string{
		"ACTA DE ESCRUTINIO - DOCUMENTO SINTETICO DE PRUEBA",
		cfg.ProcessName + " (" + cfg.ProcessCode + ")",
		"Mesa " + station,
		fmt.Sprintf("Electores habiles: %d   Votantes: %d", eligible, voters),
		fmt.Sprintf("Votos validos: %d   Nulos: %d   Blancos: %d", valid, null, blank),
	}
	for i, p := range parties {
		options[i] = map[string]any{"orden": i + 1, "codigo": p.Code, "nombre": p.Name, "partido": p.Party, "votos": votes[i]}
		lines = append(lines, fmt.Sprintf("%d. %s: %d votos", i+1, p.Name, votes[i]))
	}
	lines = append(lines, "", "Generado por /stress. Ejecucion "+runID+". No es un acta real.")

	return acta{
		Station:    station,
		Identifier: cfg.ProcessCode + "/" + station,
		Info: map[string]any{
			"version": "1.0", "mesa": station, "electores_habiles": eligible, "votantes": voters,
			"votos_validos": valid, "votos_nulos": null, "votos_blancos": blank, "opciones": options,
		},
		Data: map[string]any{
			"version": "1.0", "mesa": station, "folio": "S-" + station,
			"proceso": map[string]any{"codigo": cfg.ProcessCode, "nombre": cfg.ProcessName},
			"ubicacion": map[string]any{
				"local": "Local de prueba", "distrito": "Distrito de prueba",
				"provincia": "Provincia de prueba", "departamento": "Departamento de prueba", "ubigeo": "000000",
			},
		},
		Lines: lines,
	}
}

func b64JSON(v any) string {
	raw, _ := json.Marshal(v)
	return base64.StdEncoding.EncodeToString(raw)
}

// buildPDF escribe un PDF minimo de una pagina. Solo ASCII: el contenido es
// sintetico y no necesita fuentes incrustadas.
func buildPDF(lines []string) []byte {
	var text bytes.Buffer
	text.WriteString("BT /F1 13 Tf 50 790 Td 20 TL\n")
	for i, line := range lines {
		if i > 0 {
			text.WriteString("T*\n")
		}
		clean := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(line)
		fmt.Fprintf(&text, "(%s) Tj\n", clean)
	}
	text.WriteString("ET")

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", text.Len(), text.String()),
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

// ───────────────────────────── ejecucion ─────────────────────────────────

type row struct {
	Seq     int    `json:"seq"`
	Station string `json:"station"`
	Status  string `json:"status"`
	TrxID   string `json:"trxid"`
	Millis  int64  `json:"ms"`
	Link    string `json:"link"`
	Detail  string `json:"detail"`
}

type runner struct {
	token  string
	cfg    *config
	aead   cipher.AEAD
	client *http.Client

	mu        sync.Mutex
	running   bool
	runID     string
	total     int
	workers   int
	started   time.Time
	finished  time.Time
	rows      []row
	latencies []int64
	failures  map[string]int
	lastLink  string
	cancel    context.CancelFunc
	csvFile   *os.File
	csvOut    *csv.Writer
	runDir    string

	sent, ok, failed atomic.Int64

	statsAt time.Time
	p50     int64
	p95     int64
	pMax    int64
}

func newRunner(cfg *config) (*runner, error) {
	r := &runner{cfg: cfg, failures: map[string]int{}}
	if cfg.visorEnabled() {
		aead, err := newAEAD(cfg.CipherKey)
		if err != nil {
			return nil, err
		}
		r.aead = aead
	}
	return r, nil
}

func (r *runner) start(count, workers int, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("falta el token del API REST")
	}
	r.token = strings.TrimSpace(token)
	if r.running {
		return fmt.Errorf("ya hay una ejecucion en curso")
	}
	runID := time.Now().Format("20060102-150405")
	dir := filepath.Join(r.cfg.RunsDir, runID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("no se pudo crear la carpeta del reporte: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "results.csv"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	out := csv.NewWriter(file)
	_ = out.Write([]string{"seq", "station", "status", "trxid", "ms", "detail"})

	ctx, cancel := context.WithCancel(context.Background())
	r.running, r.runID, r.total, r.workers = true, runID, count, workers
	r.started, r.finished = time.Now(), time.Time{}
	r.rows, r.latencies, r.failures, r.lastLink = nil, make([]int64, 0, count), map[string]int{}, ""
	r.cancel, r.csvFile, r.csvOut, r.runDir = cancel, file, out, dir
	r.sent.Store(0)
	r.ok.Store(0)
	r.failed.Store(0)
	r.statsAt = time.Time{}
	r.client = &http.Client{
		Timeout:   60 * time.Second,
		Transport: &http.Transport{MaxIdleConnsPerHost: workers, MaxConnsPerHost: workers, IdleConnTimeout: 30 * time.Second},
	}

	go r.run(ctx, runID, count, workers)
	return nil
}

func (r *runner) run(ctx context.Context, runID string, count, workers int) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				r.record(r.process(ctx, runID, n))
			}
		}()
	}
feed:
	for n := 0; n < count; n++ {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- n:
		}
	}
	close(jobs)
	wg.Wait()
	r.finish()
}

// process genera, guarda en custodia y sella una acta.
func (r *runner) process(ctx context.Context, runID string, n int) row {
	began := time.Now()
	a := newActa(r.cfg, runID, n)
	result := row{Seq: n + 1, Station: a.Station}
	fail := func(detail string) row {
		result.Status, result.Detail, result.Millis = "error", detail, time.Since(began).Milliseconds()
		return result
	}

	pdf := buildPDF(a.Lines)
	sum := sha256.Sum256(pdf)
	evidence := hex.EncodeToString(sum[:])
	trx := sha1.Sum([]byte(evidence))
	result.TrxID = hex.EncodeToString(trx[:])

	if r.cfg.visorEnabled() {
		path := filepath.Join(r.cfg.CustodyPath, a.Identifier+".pdf")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fail("custodia: " + err.Error())
		}
		if err := os.WriteFile(path, pdf, 0o644); err != nil {
			return fail("custodia: " + err.Error())
		}
	}

	payload, _ := json.Marshal(map[string]any{
		"evidence": evidence, "async": "true", "transactionType": r.cfg.ProcessCode,
		"subject": "Mesa " + a.Station, "info": b64JSON(a.Info), "data": b64JSON(a.Data),
		"external_key": "ZW-" + runID + "-" + a.Station,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.BaseURL+"/stamp/", bytes.NewReader(payload))
	if err != nil {
		return fail(err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return fail("red: " + shorten(err.Error()))
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()

	var answer struct {
		Code    any    `json:"code"` // la API lo entrega como texto o como numero
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &answer)
	if code := fmt.Sprint(answer.Code); resp.StatusCode != http.StatusOK || code != "200" {
		return fail(fmt.Sprintf("http %d codigo %s %s", resp.StatusCode, code, shorten(answer.Message)))
	}

	result.Status, result.Millis = "ok", time.Since(began).Milliseconds()
	if r.aead != nil {
		if code, err := encodeCode(r.aead, a.Identifier); err == nil {
			result.Link = r.cfg.VisorURL + "/" + code
		}
	}
	return result
}

func shorten(s string) string {
	if len(s) > 90 {
		return s[:90]
	}
	return s
}

func (r *runner) record(res row) {
	r.sent.Add(1)
	if res.Status == "ok" {
		r.ok.Add(1)
	} else {
		r.failed.Add(1)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latencies = append(r.latencies, res.Millis)
	if res.Status != "ok" {
		r.failures[res.Detail]++
	} else if res.Link != "" {
		r.lastLink = res.Link
	}
	r.rows = append(r.rows, res)
	if len(r.rows) > maxRows {
		r.rows = r.rows[len(r.rows)-maxRows:]
	}
	_ = r.csvOut.Write([]string{strconv.Itoa(res.Seq), res.Station, res.Status, res.TrxID, strconv.FormatInt(res.Millis, 10), res.Detail})
}

func (r *runner) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finished = time.Now()
	r.running = false
	r.csvOut.Flush()
	r.csvFile.Close()
	r.refreshStats(true)
	summary, _ := json.MarshalIndent(r.snapshotLocked(), "", "  ")
	_ = os.WriteFile(filepath.Join(r.runDir, "summary.json"), summary, 0o640)
	r.cancel()
}

func (r *runner) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running && r.cancel != nil {
		r.cancel()
	}
}

// refreshStats recalcula percentiles como mucho cada dos segundos: con cientos
// de miles de muestras, ordenarlas en cada consulta seria un gasto innecesario.
func (r *runner) refreshStats(force bool) {
	if !force && time.Since(r.statsAt) < 2*time.Second {
		return
	}
	r.statsAt = time.Now()
	if len(r.latencies) == 0 {
		return
	}
	sorted := append([]int64(nil), r.latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	r.p50 = sorted[len(sorted)/2]
	r.p95 = sorted[int(float64(len(sorted))*0.95)-clampOne(len(sorted))]
	r.pMax = sorted[len(sorted)-1]
}

func clampOne(n int) int {
	if n < 20 {
		return 0
	}
	return 1
}

type snapshot struct {
	Running    bool           `json:"running"`
	RunID      string         `json:"run_id"`
	Total      int            `json:"total"`
	Workers    int            `json:"workers"`
	Sent       int64          `json:"sent"`
	OK         int64          `json:"ok"`
	Failed     int64          `json:"failed"`
	Elapsed    float64        `json:"elapsed_s"`
	PerSecond  float64        `json:"tx_per_s"`
	P50        int64          `json:"p50_ms"`
	P95        int64          `json:"p95_ms"`
	Max        int64          `json:"max_ms"`
	Failures   map[string]int `json:"failures"`
	LastLink   string         `json:"last_link"`
	Rows       []row          `json:"rows"`
	StartedAt  string         `json:"started_at,omitempty"`
	FinishedAt string         `json:"finished_at,omitempty"`
}

func (r *runner) snapshot() snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshStats(false)
	return r.snapshotLocked()
}

func (r *runner) snapshotLocked() snapshot {
	s := snapshot{
		Running: r.running, RunID: r.runID, Total: r.total, Workers: r.workers,
		Sent: r.sent.Load(), OK: r.ok.Load(), Failed: r.failed.Load(),
		P50: r.p50, P95: r.p95, Max: r.pMax, LastLink: r.lastLink,
		Failures: map[string]int{}, Rows: make([]row, 0, len(r.rows)),
	}
	for k, v := range r.failures {
		s.Failures[k] = v
	}
	for i := len(r.rows) - 1; i >= 0; i-- { // las mas recientes primero
		s.Rows = append(s.Rows, r.rows[i])
	}
	if !r.started.IsZero() {
		end := r.finished
		if end.IsZero() {
			end = time.Now()
		}
		s.Elapsed = end.Sub(r.started).Seconds()
		if s.Elapsed > 0 {
			s.PerSecond = float64(s.Sent) / s.Elapsed
		}
		s.StartedAt = r.started.Format(time.RFC3339)
		if !r.finished.IsZero() {
			s.FinishedAt = r.finished.Format(time.RFC3339)
		}
	}
	return s
}

// ─────────────────────────────── HTTP ────────────────────────────────────

func authenticated(cfg *config, next http.Handler) http.Handler {
	if cfg.UIPassword == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, pass, ok := req.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(cfg.UIPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="stress"`)
			http.Error(w, "acceso restringido", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("configuracion: %v", err)
	}
	run, err := newRunner(cfg)
	if err != nil {
		log.Fatalf("configuracion: %v", err)
	}
	page, _ := webFS.ReadFile("web/index.html")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{
			"process_code": cfg.ProcessCode, "process_name": cfg.ProcessName,
			"visor": cfg.visorEnabled(), "max_count": maxCount, "max_concurrency": maxConcurrency,
		})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, run.snapshot())
	})
	mux.HandleFunc("POST /api/run", func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			Count       int `json:"count"`
			Concurrency int `json:"concurrency"`
			Token       string `json:"token"`
		}
		if err := json.NewDecoder(io.LimitReader(req.Body, 1<<12)).Decode(&in); err != nil ||
			in.Count < 1 || in.Count > maxCount || in.Concurrency < 1 || in.Concurrency > maxConcurrency {
			writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("cantidad entre 1 y %d, concurrencia entre 1 y %d", maxCount, maxConcurrency)})
			return
		}
		if err := run.start(in.Count, in.Concurrency, in.Token); err != nil {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 202, map[string]string{"status": "iniciada"})
	})
	mux.HandleFunc("POST /api/stop", func(w http.ResponseWriter, _ *http.Request) {
		run.stop()
		writeJSON(w, 200, map[string]string{"status": "detenida"})
	})
	mux.HandleFunc("GET /api/report.csv", func(w http.ResponseWriter, req *http.Request) {
		run.mu.Lock()
		dir, active := run.runDir, run.running
		run.mu.Unlock()
		if dir == "" || active {
			http.Error(w, "no hay un reporte terminado", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="stress-`+filepath.Base(dir)+`.csv"`)
		http.ServeFile(w, req, filepath.Join(dir, "results.csv"))
	})

	log.Printf("stress escuchando en http://%s | proceso %s | visor: %v", cfg.Listen, cfg.ProcessCode, cfg.visorEnabled())
	log.Fatal((&http.Server{Addr: cfg.Listen, Handler: authenticated(cfg, mux), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
}
