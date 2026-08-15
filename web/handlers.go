package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/zeroAlcBeer/bangou/checker"
	"github.com/zeroAlcBeer/bangou/committed"
	"github.com/zeroAlcBeer/bangou/provider"
	"github.com/zeroAlcBeer/bangou/staging"
)

type Handlers struct {
	registry *Registry
	store    committed.Store

	libRescrape sync.Map // number -> *LibRescrapeResult
}

type LibRescrapeResult struct {
	Status string
	Old    *committed.Metadata
	New    *provider.MovieMetadata
	Errors map[string]string
}

// helper to resolve pipeline runtime from URL path
func (h *Handlers) getRuntime(r *http.Request) (*PipelineRuntime, int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid pipeline id")
	}
	rt := h.registry.Get(id)
	if rt == nil {
		return nil, id, fmt.Errorf("pipeline %d not found", id)
	}
	return rt, id, nil
}

// ── Pipelines ──

type PipelineResponse struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	InputDir         string   `json:"inputDir"`
	OutputDir        string   `json:"outputDir"`
	PathPattern      string   `json:"pathPattern"`
	ArchiveDir       string   `json:"archiveDir"`
	EnableMerge      bool     `json:"enableMerge"`
	DownloadProvider string   `json:"downloadProvider"`
	ScrapeProviders  []string `json:"scrapeProviders"`
	PendingCount     int      `json:"pendingCount"`
	LibraryCount     int      `json:"libraryCount"`
	Status           string   `json:"status"`
}

func (h *Handlers) ListPipelines(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pipes, err := h.store.ListPipelines(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := make([]PipelineResponse, 0, len(pipes))
	for _, p := range pipes {
		_, libCount, _ := h.store.ListBangousByPipeline(ctx, p.ID, 0, 0, "", "", "", "")
		pending := 0
		if rt := h.registry.Get(p.ID); rt != nil {
			pending = len(rt.Manager.ListGroups()) + len(rt.Manager.ListUnknowns())
		}
		out = append(out, PipelineResponse{
			ID: p.ID, Name: p.Name, InputDir: p.InputDir, OutputDir: p.OutputDir,
			PathPattern: p.PathPattern, ArchiveDir: p.ArchiveDir, EnableMerge: p.EnableMerge,
			DownloadProvider: p.DownloadProvider, ScrapeProviders: splitProviders(p.ScrapeProviders),
			PendingCount: pending, LibraryCount: libCount, Status: "idle",
		})
	}
	writeOK(w, out)
}

func (h *Handlers) CreatePipeline(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string   `json:"name"`
		InputDir         string   `json:"inputDir"`
		OutputDir        string   `json:"outputDir"`
		PathPattern      string   `json:"pathPattern"`
		ArchiveDir       string   `json:"archiveDir"`
		EnableMerge      bool     `json:"enableMerge"`
		DownloadProvider string   `json:"downloadProvider"`
		ScrapeProviders  []string `json:"scrapeProviders"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if req.Name == "" || req.InputDir == "" || req.OutputDir == "" {
		writeError(w, 400, "name, inputDir, outputDir required")
		return
	}
	if req.PathPattern == "" {
		req.PathPattern = "{Number}"
	}
	if len(req.ScrapeProviders) == 0 {
		req.ScrapeProviders = []string{"avwiki", "dmm"}
	}

	p := &committed.Pipeline{
		Name: req.Name, InputDir: req.InputDir, OutputDir: req.OutputDir,
		PathPattern: req.PathPattern, ArchiveDir: req.ArchiveDir, EnableMerge: req.EnableMerge,
		DownloadProvider: req.DownloadProvider, ScrapeProviders: strings.Join(req.ScrapeProviders, ","),
	}
	id, err := h.store.CreatePipeline(r.Context(), p)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, 409, "input directory already in use")
			return
		}
		writeError(w, 500, err.Error())
		return
	}
	p.ID = id
	_ = h.registry.StartPipeline(*p)
	writeJSON(w, 201, map[string]int64{"id": id})
}

func (h *Handlers) DeletePipeline(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}
	_, total, err := h.store.ListBangousByPipeline(r.Context(), id, 0, 0, "", "", "", "")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if total > 0 {
		writeError(w, 409, "pipeline has linked bangous; unlink first")
		return
	}
	h.registry.StopPipeline(id)
	if err := h.store.DeletePipeline(r.Context(), id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeOK(w, map[string]string{"status": "deleted"})
}

// ── Groups ──

type GroupResponse struct {
	Number       string         `json:"number"`
	Items        []ItemResponse `json:"items"`
	TotalSizeGB  float64        `json:"totalSizeGB"`
	Scrape       ScrapeResponse `json:"scrape"`
	Task         string         `json:"task"`
	TaskErr      string         `json:"taskErr,omitempty"`
	TaskProgress int            `json:"taskProgress"`
	AllReady     bool           `json:"allReady"`
}

type ItemResponse struct {
	Path           string  `json:"path"`
	Filename       string  `json:"filename"`
	Part           int     `json:"part"`
	SizeGB         float64 `json:"sizeGB"`
	Ready          bool    `json:"ready"`
	Resolution     string  `json:"resolution,omitempty"`
	VideoCodec     string  `json:"videoCodec,omitempty"`
	AudioCodec     string  `json:"audioCodec,omitempty"`
	Bitrate        string  `json:"bitrate,omitempty"`
	Duration       string  `json:"duration,omitempty"`
	DownloadPct    int     `json:"downloadPct"`
	DownloadStatus string  `json:"downloadStatus,omitempty"`
}

type ScrapeResponse struct {
	Meta   *MetaResponse     `json:"meta"`
	Errors map[string]string `json:"errors,omitempty"`
	Status string            `json:"status"`
}

type MetaResponse struct {
	Number         string   `json:"number"`
	Title          string   `json:"title"`
	Director       string   `json:"director,omitempty"`
	Maker          string   `json:"maker,omitempty"`
	Label          string   `json:"label,omitempty"`
	Series         string   `json:"series,omitempty"`
	Actors         []string `json:"actors,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	CoverURL       string   `json:"coverURL,omitempty"`
	SampleImages   []string `json:"sampleImages,omitempty"`
	Premiered      string   `json:"premiered,omitempty"`
	Year           string   `json:"year,omitempty"`
	Runtime        string   `json:"runtime,omitempty"`
	Rating         string   `json:"rating,omitempty"`
	ReviewCount    int      `json:"reviewCount"`
	SampleMovieURL string   `json:"sampleMovieURL,omitempty"`
	PageURL        string   `json:"pageURL,omitempty"`
	Provider       string   `json:"provider,omitempty"`
}

type UnknownResponse struct {
	Path     string  `json:"path"`
	Filename string  `json:"filename"`
	SizeGB   float64 `json:"sizeGB"`
}

type GroupsPageResponse struct {
	Groups   []GroupResponse   `json:"groups"`
	Unknowns []UnknownResponse `json:"unknowns"`
}

func (h *Handlers) ListGroups(w http.ResponseWriter, r *http.Request) {
	rt, _, err := h.getRuntime(r)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	groups := rt.Manager.ListGroups()
	unknowns := rt.Manager.ListUnknowns()

	grs := make([]GroupResponse, 0, len(groups))
	for _, g := range groups {
		grs = append(grs, buildGroupResponse(g))
	}

	urs := make([]UnknownResponse, 0, len(unknowns))
	for _, u := range unknowns {
		urs = append(urs, UnknownResponse{Path: u.Path, Filename: u.Filename, SizeGB: float64(u.Size) / (1024 * 1024 * 1024)})
	}
	writeOK(w, GroupsPageResponse{Groups: grs, Unknowns: urs})
}

func buildGroupResponse(g staging.StagingGroup) GroupResponse {
	gr := GroupResponse{Number: g.Number, Task: g.Task, TaskErr: g.TaskErr, TaskProgress: g.TaskProgress, AllReady: true}
	var totalSize int64
	items := make([]ItemResponse, 0, len(g.Items))
	for _, item := range g.Items {
		totalSize += item.File.Size
		if !item.File.Ready {
			gr.AllReady = false
		}
		ir := ItemResponse{
			Path: item.File.Path, Filename: item.File.Filename, Part: item.Parsed.Part,
			SizeGB: float64(item.File.Size) / (1024 * 1024 * 1024), Ready: item.File.Ready,
			DownloadPct: item.File.DownloadPct, DownloadStatus: item.File.DownloadStatus,
		}
		if item.File.Media != nil {
			m := item.File.Media
			ir.Resolution = m.Resolution()
			ir.VideoCodec = m.VideoCodec
			ir.AudioCodec = m.AudioCodec
			ir.Bitrate = m.BitrateText()
			ir.Duration = m.DurationText()
		}
		items = append(items, ir)
	}
	gr.Items = items
	gr.TotalSizeGB = float64(totalSize) / (1024 * 1024 * 1024)
	gr.Scrape = ScrapeResponse{Errors: g.Scrape.Errors, Status: g.Scrape.Status}
	if g.Scrape.Meta != nil {
		mm := g.Scrape.Meta
		gr.Scrape.Meta = &MetaResponse{
			Number: mm.Number, Title: mm.Title, Director: mm.Director, Maker: mm.Maker, Label: mm.Label,
			Series: mm.Series, Actors: mm.Actors, Genres: mm.Genres, CoverURL: mm.CoverURL,
			SampleImages: mm.SampleImages, Premiered: mm.Premiered, Year: mm.Year,
			Runtime: mm.Runtime, Rating: mm.Rating, ReviewCount: mm.ReviewCount,
			SampleMovieURL: mm.SampleMovieURL, PageURL: mm.PageURL, Provider: mm.Provider,
		}
	}
	return gr
}

// ── Group Actions ──

func (h *Handlers) GroupLink(w http.ResponseWriter, r *http.Request) {
	rt, _, err := h.getRuntime(r)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	number := strings.ToUpper(strings.TrimSpace(r.PathValue("number")))
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Paths) == 0 {
		writeError(w, 400, "paths required")
		return
	}
	if err := validateSingleExtensionPaths(req.Paths); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	rt.Manager.SetTask(number, "linking", "")
	if err := rt.Executor.Link(r.Context(), number, req.Paths, rt.LinkOpts()); err != nil {
		log.Printf("[link] %s: error: %v", number, err)
		rt.Manager.SetTask(number, "error", err.Error())
		writeError(w, 500, err.Error())
		return
	}
	writeOK(w, map[string]string{"status": "linked"})
}

func (h *Handlers) GroupMerge(w http.ResponseWriter, r *http.Request) {
	rt, _, err := h.getRuntime(r)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	number := strings.ToUpper(strings.TrimSpace(r.PathValue("number")))
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Paths) < 2 {
		writeError(w, 400, "at least 2 paths required")
		return
	}
	group := rt.Manager.GetGroup(number)
	if group == nil {
		writeError(w, 404, "group not found")
		return
	}
	if hasMixedMKVAndMP4Group(group.Items) {
		writeError(w, 400, "merge is disabled for mixed mkv/mp4 groups")
		return
	}
	if !rt.mergeMu.TryLock() {
		writeError(w, 409, "another merge is already running")
		return
	}
	rt.Manager.SetTask(number, "merging", "")
	release := h.registry.TrackTask()
	go func() {
		defer release()
		defer rt.mergeMu.Unlock()
		if err := rt.Executor.Merge(context.Background(), number, req.Paths); err != nil {
			log.Printf("[merge] %s: error: %v", number, err)
			rt.Manager.SetTask(number, "error", err.Error())
			return
		}
		rt.Manager.SetTask(number, "", "")
		go rt.Scan(context.Background(), h.store)
	}()
	writeOK(w, map[string]string{"status": "merging"})
}

func (h *Handlers) GroupRescrape(w http.ResponseWriter, r *http.Request) {
	rt, _, err := h.getRuntime(r)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	number := strings.ToUpper(strings.TrimSpace(r.PathValue("number")))
	rt.Rescrape(number)
	writeOK(w, map[string]string{"status": "scraping"})
}

func (h *Handlers) ManualTag(w http.ResponseWriter, r *http.Request) {
	rt, _, err := h.getRuntime(r)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	number := strings.ToUpper(strings.TrimSpace(r.PathValue("number")))
	var req struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil || req.Path == "" {
		writeError(w, 400, "path required")
		return
	}
	rt.Manager.ManualTag(req.Path, number)
	writeOK(w, map[string]string{"status": "tagged"})
}

// ── Unknown Actions ──

func (h *Handlers) UnknownTag(w http.ResponseWriter, r *http.Request) {
	rt, _, err := h.getRuntime(r)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	var req struct {
		Path   string `json:"path"`
		Number string `json:"number"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	req.Number = strings.ToUpper(strings.TrimSpace(req.Number))
	if req.Path == "" || req.Number == "" {
		writeError(w, 400, "path and number required")
		return
	}
	rt.Manager.ManualTag(req.Path, req.Number)
	writeOK(w, map[string]string{"status": "tagged"})
}

// ── Scan / Link All ──

func (h *Handlers) TriggerScan(w http.ResponseWriter, r *http.Request) {
	rt, _, err := h.getRuntime(r)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	go rt.Scan(context.Background(), h.store)
	writeOK(w, map[string]string{"status": "scanning"})
}

// ── Library ──

type LibraryFileResponse struct {
	ID              int64  `json:"id"`
	SrcPath         string `json:"srcPath"`
	LinkPath        string `json:"linkPath"`
	LinkType        string `json:"linkType"`
	FileSize        int64  `json:"fileSize"`
	Resolution      string `json:"resolution,omitempty"`
	VideoCodec      string `json:"videoCodec,omitempty"`
	AudioCodec      string `json:"audioCodec,omitempty"`
	Duration        string `json:"duration,omitempty"`
	Bitrate         string `json:"bitrate,omitempty"`
	Alive           bool   `json:"alive"`
	SourceAvailable bool   `json:"sourceAvailable"`
}

type LibraryBangouResponse struct {
	ID             int64                 `json:"id"`
	Number         string                `json:"number"`
	Files          []LibraryFileResponse `json:"outputs"` // JSON key kept as "outputs" for frontend compat
	NFOPath        string                `json:"nfoPath,omitempty"`
	CoverPath      string                `json:"coverPath,omitempty"`
	RawPath        string                `json:"rawPath,omitempty"`
	Title          string                `json:"title,omitempty"`
	Actors         string                `json:"actors,omitempty"`
	Genres         []string              `json:"genres,omitempty"`
	CoverURL       string                `json:"coverURL,omitempty"`
	SampleImages   []string              `json:"sampleImages,omitempty"`
	Rating         string                `json:"rating,omitempty"`
	ReviewCount    int                   `json:"reviewCount"`
	PageURL        string                `json:"pageURL,omitempty"`
	Maker          string                `json:"maker,omitempty"`
	Label          string                `json:"label,omitempty"`
	Series         string                `json:"series,omitempty"`
	Director       string                `json:"director,omitempty"`
	SampleMovieURL string                `json:"sampleMovieURL,omitempty"`
	Premiered      string                `json:"premiered,omitempty"`
	Year           string                `json:"year,omitempty"`
	Runtime        string                `json:"runtime,omitempty"`
	Provider       string                `json:"provider,omitempty"`
}

type LibraryPageResponse struct {
	Items []LibraryBangouResponse `json:"items"`
	Total int                     `json:"total"`
	Page  int                     `json:"page"`
	Size  int                     `json:"size"`
}

func (h *Handlers) ListLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid pipeline id")
		return
	}
	ctx := r.Context()
	checker.CheckAll(ctx, h.store)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size <= 0 || size > 100 {
		size = 12
	}
	if page < 0 {
		page = 0
	}

	sort := r.URL.Query().Get("sort")   // "added", "number", "year", "rating"
	order := r.URL.Query().Get("order") // "asc", "desc"
	status := r.URL.Query().Get("status")
	q := r.URL.Query().Get("q")
	bangous, total, err := h.store.ListBangousByPipeline(ctx, id, size, page*size, sort, order, status, q)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	items := make([]LibraryBangouResponse, 0, len(bangous))
	for _, b := range bangous {
		lv := LibraryBangouResponse{ID: b.ID, Number: b.Number, NFOPath: b.NFOPath, CoverPath: b.CoverPath, RawPath: b.RawPath}

		files, _ := h.store.ListBangouFilesByBangou(ctx, b.ID)
		lv.Files = make([]LibraryFileResponse, 0, len(files))
		for _, f := range files {
			sourceAvailable := false
			if f.SrcPath != "" {
				if _, err := os.Stat(f.SrcPath); err == nil {
					sourceAvailable = true
				}
			}
			lv.Files = append(lv.Files, LibraryFileResponse{
				ID: f.ID, SrcPath: f.SrcPath, LinkPath: f.LinkPath,
				LinkType: f.LinkType, FileSize: f.FileSize, Resolution: f.Resolution,
				VideoCodec: f.VideoCodec, AudioCodec: f.AudioCodec, Duration: f.Duration,
				Bitrate: f.Bitrate, Alive: f.Alive, SourceAvailable: sourceAvailable,
			})
		}

		if meta, _ := h.store.GetMetadataByBangou(ctx, b.ID); meta != nil {
			lv.Title = meta.Title
			lv.Actors = meta.Actors
			lv.CoverURL = meta.CoverURL
			lv.Provider = meta.Provider
			lv.Rating = meta.Rating
			lv.ReviewCount = meta.ReviewCount
			lv.PageURL = meta.PageURL
			lv.Maker = meta.Maker
			lv.Label = meta.Label
			lv.Series = meta.Series
			lv.Director = meta.Director
			lv.SampleMovieURL = meta.SampleMovieURL
			lv.Premiered = meta.Premiered
			lv.Year = meta.Year
			lv.Runtime = meta.Runtime
			if meta.Genres != "" {
				lv.Genres = strings.Split(meta.Genres, ",")
			}
			if meta.SampleImages != "" {
				lv.SampleImages = strings.Split(meta.SampleImages, ",")
			}
		}
		items = append(items, lv)
	}
	writeOK(w, LibraryPageResponse{Items: items, Total: total, Page: page, Size: size})
}

// ── Library Rescrape ──

func (h *Handlers) LibraryRescrape(w http.ResponseWriter, r *http.Request) {
	number := strings.ToUpper(strings.TrimSpace(r.PathValue("number")))
	if _, loaded := h.libRescrape.LoadOrStore(number, &LibRescrapeResult{Status: "scraping"}); loaded {
		writeError(w, 409, "rescrape already in progress")
		return
	}
	// Use first available runtime for scraping
	runtimes := h.registry.All()
	if len(runtimes) == 0 {
		h.libRescrape.Delete(number)
		writeError(w, 500, "no pipeline running")
		return
	}
	rt := runtimes[0]
	go func() {
		meta, errs := rt.LibScrapeFn(context.Background(), h.store, number)
		if meta != nil {
			// Find old metadata from the first matching bangou
			var old *committed.Metadata
			if b, _ := h.store.GetBangouByPipelineAndNumber(context.Background(), rt.Pipeline.ID, number); b != nil {
				old, _ = h.store.GetMetadataByBangou(context.Background(), b.ID)
			}
			h.libRescrape.Store(number, &LibRescrapeResult{Status: "done", Old: old, New: meta, Errors: errs})
		} else {
			h.libRescrape.Store(number, &LibRescrapeResult{Status: "failed", Errors: errs})
		}
	}()
	writeOK(w, map[string]string{"status": "scraping"})
}

func (h *Handlers) LibraryRescrapeApply(w http.ResponseWriter, r *http.Request) {
	number := strings.ToUpper(strings.TrimSpace(r.PathValue("number")))
	v, ok := h.libRescrape.Load(number)
	if !ok {
		writeError(w, 404, "no pending rescrape")
		return
	}
	res := v.(*LibRescrapeResult)
	if res.Status != "done" || res.New == nil {
		writeError(w, 400, "rescrape not ready")
		return
	}
	// Apply to all bangous matching this number across pipelines
	for _, rt := range h.registry.All() {
		b, _ := h.store.GetBangouByPipelineAndNumber(r.Context(), rt.Pipeline.ID, number)
		if b == nil {
			continue
		}
		_ = h.store.UpsertMetadata(r.Context(), &committed.Metadata{
			BangouID: b.ID,
			Number:   number, Title: res.New.Title, Plot: res.New.Plot,
			Director: res.New.Director, Maker: res.New.Maker, Label: res.New.Label,
			Series: res.New.Series, Actors: strings.Join(res.New.Actors, ","),
			Genres: strings.Join(res.New.Genres, ","), CoverURL: res.New.CoverURL,
			SampleImages: strings.Join(res.New.SampleImages, ","),
			Premiered:    res.New.Premiered, Year: res.New.Year, Runtime: res.New.Runtime,
			Rating: res.New.Rating, ReviewCount: res.New.ReviewCount,
			SampleMovieURL: res.New.SampleMovieURL,
			PageURL:        res.New.PageURL, ContentID: res.New.ContentID, Provider: res.New.Provider,
		})
	}
	h.libRescrape.Delete(number)
	writeOK(w, map[string]string{"status": "applied"})
}

func (h *Handlers) LibraryRescrapeDismiss(w http.ResponseWriter, r *http.Request) {
	number := strings.ToUpper(strings.TrimSpace(r.PathValue("number")))
	h.libRescrape.Delete(number)
	writeOK(w, map[string]string{"status": "dismissed"})
}

// ── Bangou Actions ──

func (h *Handlers) UnlinkBangou(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}
	bangou, err := h.store.GetBangou(r.Context(), id)
	if err != nil || bangou == nil {
		writeError(w, 404, "bangou not found")
		return
	}
	rt := h.registry.Get(bangou.PipelineID)
	if rt == nil {
		writeError(w, 404, "pipeline runtime not found")
		return
	}
	files, err := h.store.ListBangouFilesByBangou(r.Context(), bangou.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	for _, f := range files {
		if err := rt.Executor.Unlink(r.Context(), &f); err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}
	rt.Manager.RemoveGroup(bangou.Number)
	go rt.Scan(context.Background(), h.store)
	writeOK(w, map[string]string{"status": "unlinked"})
}

func (h *Handlers) RestoreBangou(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}
	bangou, err := h.store.GetBangou(r.Context(), id)
	if err != nil || bangou == nil {
		writeError(w, 404, "bangou not found")
		return
	}
	rt := h.registry.Get(bangou.PipelineID)
	if rt == nil {
		writeError(w, 404, "pipeline runtime not found")
		return
	}
	files, err := h.store.ListBangouFilesByBangou(r.Context(), bangou.ID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	restored := 0
	for _, f := range files {
		if f.Alive {
			continue
		}
		if err := rt.Executor.RestoreLink(r.Context(), &f); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		restored++
	}
	writeOK(w, map[string]any{"status": "restored", "restored": restored})
}

func (h *Handlers) BackToPendingBangou(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}
	bangou, err := h.store.GetBangou(r.Context(), id)
	if err != nil || bangou == nil {
		writeError(w, 404, "bangou not found")
		return
	}
	rt := h.registry.Get(bangou.PipelineID)
	if rt == nil {
		writeError(w, 404, "pipeline runtime not found")
		return
	}
	if err := h.store.DeleteBangou(r.Context(), bangou.ID); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	go rt.Scan(context.Background(), h.store)
	writeOK(w, map[string]string{"status": "pending"})
}

// ── Provider Configs ──

func (h *Handlers) ListProviderConfigs(w http.ResponseWriter, r *http.Request) {
	configs, err := h.store.ListProviderConfigs(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeOK(w, configs)
}

func (h *Handlers) SetProviderConfig(w http.ResponseWriter, r *http.Request) {
	prov := r.PathValue("provider")
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	raw, _ := encodeJSON(req)
	if err := h.store.SetProviderConfig(r.Context(), prov, raw); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeOK(w, map[string]string{"status": "saved"})
}

func (h *Handlers) TestProviderConfig(w http.ResponseWriter, r *http.Request) {
	prov := r.PathValue("provider")
	ctx := r.Context()
	switch prov {
	case "dmm":
		cfgStr, _ := h.store.GetProviderConfig(ctx, "dmm")
		var cfg struct {
			APIID       string `json:"api_id"`
			AffiliateID string `json:"affiliate_id"`
		}
		_ = decodeJSON(cfgStr, &cfg)
		if cfg.APIID == "" || cfg.AffiliateID == "" {
			writeError(w, 400, "API ID and Affiliate ID required")
			return
		}
		p := provider.NewDMM(cfg.APIID, cfg.AffiliateID)
		_, err := p.Scrape(ctx, provider.Predict{Number: "SIVR-476"})
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeOK(w, map[string]string{"status": "ok"})
	case "aria2":
		writeOK(w, map[string]string{"status": "ok"})
	default:
		writeError(w, 400, "unknown provider")
	}
}

// ── Helpers ──

func splitProviders(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validateSingleExtensionPaths(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("paths required")
	}
	extSet := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		if ext == "" {
			return fmt.Errorf("missing extension in selection")
		}
		extSet[ext] = struct{}{}
		if len(extSet) > 1 {
			return fmt.Errorf("link selection must use one extension")
		}
	}
	return nil
}

// ── Browse Directory ──

type BrowseEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

func (h *Handlers) BrowseDirectory(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	dirs := make([]BrowseEntry, 0)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, BrowseEntry{
				Name:  e.Name(),
				Path:  filepath.Join(dir, e.Name()),
				IsDir: true,
			})
		}
	}
	writeOK(w, dirs)
}

func hasMixedMKVAndMP4Group(items []staging.StagedItem) bool {
	hasMKV := false
	hasMP4 := false
	for _, item := range items {
		ext := strings.ToLower(filepath.Ext(item.File.Filename))
		if ext == "" {
			ext = strings.ToLower(filepath.Ext(item.File.Path))
		}
		switch ext {
		case ".mkv":
			hasMKV = true
		case ".mp4":
			hasMP4 = true
		}
		if hasMKV && hasMP4 {
			return true
		}
	}
	return false
}
