package docs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

const (
	pdfSchematicRenderDPI   = 250
	pdfSchematicMaxPages    = 30
	pdfScanMaxPages         = 10
	pdfSchematicRenderLimit = 90 * time.Second
	pdfMaxImageSizeBytes    = 15 << 20
)

// Хуки для подмены поиска бинарников poppler-utils в юнит-тестах.
var (
	pdfPpmLookPath    = exec.LookPath
	pdfImagesLookPath = exec.LookPath
	pdfInfoLookPath   = exec.LookPath
)

// PdfSchematicImage — отрендеренная страница PDF, распознанная как схема или чертёж.
type PdfSchematicImage struct {
	PageNumber int
	Filename   string
	Data       []byte
}

// PdfImportResult — результат разбора PDF: постраничный Markdown и извлечённые схемы.
type PdfImportResult struct {
	Pages      []string
	Schematics []PdfSchematicImage
}

// ExtractPdfWithSchematics извлекает текст PDF постранично и автоматически рендерит
// страницы со схемами/чертежами в PNG (250 DPI через pdftoppm), чтобы они сохранялись
// во вложениях, отображались в документе и индексировались Vision-анализатором.
func ExtractPdfWithSchematics(ctx context.Context, data []byte) (*PdfImportResult, error) {
	if !hasPDFHeader(data) {
		return nil, platform.NewValidation("Некорректный .pdf файл", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var raw string
	var err error
	if bin, lookErr := pdfTextLookPath("pdftotext"); lookErr == nil && bin != "" {
		raw, err = extractPdfWithPoppler(ctx, bin, data)
	} else {
		raw, err = extractPdfTextNative(data)
	}
	if err != nil {
		return nil, err
	}

	pages := splitPdfPages(raw)
	hasAnyText := false
	for _, p := range pages {
		if p != "" {
			hasAnyText = true
			break
		}
	}

	// Определяем страницы со схемами и при наличии pdftoppm рендерим их в PNG.
	schematics := extractPdfSchematicPages(ctx, data, pages, hasAnyText)

	if !hasAnyText && len(schematics) == 0 {
		return nil, platform.NewValidation(
			"Не удалось извлечь текст и схемы из PDF: возможно, файл является сканом без текстового слоя, а pdftoppm недоступен",
			nil,
		)
	}

	return &PdfImportResult{
		Pages:      pages,
		Schematics: schematics,
	}, nil
}

// splitPdfPages разбивает сырой вывод pdftotext по разрыву страниц (\f) и форматирует каждую страницу.
func splitPdfPages(raw string) []string {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	rawPages := strings.Split(normalized, "\f")
	// pdftotext добавляет завершающий \f после последней страницы — убираем пустой хвост.
	if len(rawPages) > 1 && strings.TrimSpace(rawPages[len(rawPages)-1]) == "" {
		rawPages = rawPages[:len(rawPages)-1]
	}
	pages := make([]string, len(rawPages))
	for i, p := range rawPages {
		pages[i] = cleanPdfPage(p)
	}
	return pages
}

// strongSchematicPhrases — однозначные заголовки схем и чертежей (RU/EN).
var strongSchematicPhrases = []string{
	"электрическая схема",
	"принципиальная схема",
	"схема подключения",
	"монтажная схема",
	"схема соединений",
	"схема электропроводки",
	"гидравлическая схема",
	"функциональная схема",
	"электрическая принципиальная",
	"wiring diagram",
	"electrical diagram",
	"electrical schematic",
	"circuit diagram",
	"connection diagram",
	"hydraulic diagram",
	"schematic diagram",
}

// diagramAnchorWords — признаки чертежа/клеммника в сочетании с техническими терминами.
var diagramAnchorWords = []string{
	"плата управления",
	"клеммн",
	"клемма",
	"рис.",
	"схема",
	"xt1",
	"xt2",
	"xt3",
	"x12",
	"terminal",
	"pcb",
}

var diagramTechTerms = []string{
	"термостат",
	"газовый клапан",
	"клапан",
	"насос",
	"датчик",
	"электрод",
	"розжиг",
	"ионизац",
	"выключатель",
	"реле",
	"предохранитель",
	"фаза",
	"нейтраль",
	"заземлен",
	"230в",
	"220в",
	"pwm",
	"ntc",
	"honeywell",
	"sit ",
	"m1",
	"m2",
}

// isSchematicPageText определяет по тексту страницы, содержит ли она электрическую/техническую схему.
func isSchematicPageText(pageText string) bool {
	low := strings.ToLower(pageText)
	if low == "" {
		return false
	}
	for _, phrase := range strongSchematicPhrases {
		if strings.Contains(low, phrase) {
			return true
		}
	}
	hasAnchor := false
	for _, w := range diagramAnchorWords {
		if strings.Contains(low, w) {
			hasAnchor = true
			break
		}
	}
	if !hasAnchor {
		return false
	}
	hits := 0
	for _, t := range diagramTechTerms {
		if strings.Contains(low, t) {
			hits++
			if hits >= 2 {
				return true
			}
		}
	}
	return false
}

func extractPdfSchematicPages(ctx context.Context, data []byte, pages []string, hasAnyText bool) []PdfSchematicImage {
	ppmBin, err := pdfPpmLookPath("pdftoppm")
	if err != nil || ppmBin == "" {
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "tsfera-pdf-schem-*")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "source.pdf")
	if err := os.WriteFile(pdfPath, data, 0o600); err != nil {
		return nil
	}

	totalPages := len(pages)
	if n := detectPdfPageCount(ctx, pdfPath); n > totalPages {
		totalPages = n
	}
	if totalPages == 0 {
		totalPages = 1
	}

	largeImgPages := detectLargeImagePages(ctx, pdfPath)

	candidateSet := map[int]bool{}
	if !hasAnyText {
		// Чистый скан без текстового слоя (например, отсканированная схема котла) — рендерим первые страницы.
		limit := totalPages
		if limit > pdfScanMaxPages {
			limit = pdfScanMaxPages
		}
		for p := 1; p <= limit; p++ {
			candidateSet[p] = true
		}
	} else {
		for idx, text := range pages {
			pageNum := idx + 1
			nextText := ""
			if idx+1 < len(pages) {
				nextText = pages[idx+1]
			}
			if isSchematicPageText(text) || isBlueprintDiagramPage(text, nextText) {
				candidateSet[pageNum] = true
				continue
			}
			// Страница с крупным встроенным изображением и коротким текстом (типичная схема/чертёж-картинка).
			if largeImgPages[pageNum] && len([]rune(text)) < 450 {
				candidateSet[pageNum] = true
			}
		}
	}

	if len(candidateSet) == 0 {
		return nil
	}

	pageNums := make([]int, 0, len(candidateSet))
	for p := range candidateSet {
		if p >= 1 && p <= totalPages {
			pageNums = append(pageNums, p)
		}
	}
	sort.Ints(pageNums)
	if len(pageNums) > pdfSchematicMaxPages {
		pageNums = pageNums[:pdfSchematicMaxPages]
	}

	renderCtx, cancel := context.WithTimeout(ctx, pdfSchematicRenderLimit)
	defer cancel()

	out := make([]PdfSchematicImage, 0, len(pageNums))
	for _, p := range pageNums {
		prefix := filepath.Join(tmpDir, fmt.Sprintf("page_%03d", p))
		cmd := exec.CommandContext(
			renderCtx,
			ppmBin,
			"-png",
			"-r", strconv.Itoa(pdfSchematicRenderDPI),
			"-f", strconv.Itoa(p),
			"-l", strconv.Itoa(p),
			"-singlefile",
			pdfPath,
			prefix,
		)
		if err := cmd.Run(); err != nil {
			continue
		}
		pngPath := prefix + ".png"
		imgBytes, err := os.ReadFile(pngPath)
		if err != nil || len(imgBytes) == 0 || len(imgBytes) > pdfMaxImageSizeBytes {
			continue
		}
		out = append(out, PdfSchematicImage{
			PageNumber: p,
			Filename:   fmt.Sprintf("schematic_page_%02d.png", p),
			Data:       imgBytes,
		})
	}
	return out
}

func detectPdfPageCount(ctx context.Context, pdfPath string) int {
	bin, err := pdfInfoLookPath("pdfinfo")
	if err != nil || bin == "" {
		return 0
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(runCtx, bin, pdfPath).Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "pages:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				if n, convErr := strconv.Atoi(strings.TrimSpace(parts[1])); convErr == nil && n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

// detectLargeImagePages вызывает `pdfimages -list` и возвращает номера страниц с крупными изображениями (>= 400x300).
func detectLargeImagePages(ctx context.Context, pdfPath string) map[int]bool {
	res := map[int]bool{}
	bin, err := pdfImagesLookPath("pdfimages")
	if err != nil || bin == "" {
		return res
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	cmd := exec.CommandContext(runCtx, bin, "-list", pdfPath)
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return res
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		fields := strings.Fields(line)
		// Формат pdfimages -list: page num type width height color comp bpc enc interp object ID ...
		if len(fields) < 5 {
			continue
		}
		page, errP := strconv.Atoi(fields[0])
		width, errW := strconv.Atoi(fields[3])
		height, errH := strconv.Atoi(fields[4])
		if errP != nil || errW != nil || errH != nil {
			continue
		}
		if page > 0 && width >= 400 && height >= 300 {
			res[page] = true
		}
	}
	return res
}

// buildPdfMarkdownWithSchematics собирает итоговый Markdown документа, встраивая сохранённые
// схемы-вложения непосредственно после текста их страницы.
func buildPdfMarkdownWithSchematics(pages []string, schematicsByPage map[int][]Attachment) string {
	maxPage := len(pages)
	for p := range schematicsByPage {
		if p > maxPage {
			maxPage = p
		}
	}
	blocks := make([]string, 0, maxPage)
	for p := 1; p <= maxPage; p++ {
		var parts []string
		if p <= len(pages) {
			if cleaned := sanitizePdfPageForMarkdown(pages[p-1]); cleaned != "" {
				parts = append(parts, cleaned)
			}
		}
		if atts := schematicsByPage[p]; len(atts) > 0 {
			for _, att := range atts {
				parts = append(parts, fmt.Sprintf(
					"![Схема (стр. %d): %s](/api/v1/docs/attachments/%d/download?inline=1)",
					p, att.Filename, att.ID,
				))
			}
		}
		if len(parts) > 0 {
			blocks = append(blocks, strings.Join(parts, "\n\n"))
		}
	}
	return strings.TrimSpace(strings.Join(blocks, "\n\n---\n\n"))
}
