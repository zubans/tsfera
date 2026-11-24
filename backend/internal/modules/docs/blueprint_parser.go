package docs

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// BlueprintMetadata — сводные метаданные чертежа/каталога котла, извлечённые при импорте PDF.
type BlueprintMetadata struct {
	Brand  string
	Model  string
	Parts  []BoilerPart
	IsSpec bool
}

var knownBoilerBrands = []struct {
	canonical string
	patterns  []string
}{
	{"Vaillant", []string{"vaillant", "ecovit", "ecocompact", "ecotec", "atmovit", "atmotec", "turbotec", "котёл vk ", "котел vk ", "vk int ", "vk/vks"}},
	{"Kentatsu", []string{"kentatsu", "nobby smart", "nobby balance", "nobby electro", "impect", "kobold"}},
	{"BAXI", []string{"baxi", "luna-3", "luna duo-tec", "ecofour", "eco-4s", "eco life", "eco nova", "nuvola", "mainfour", "baxi slim"}},
	{"Daesung", []string{"daesung"}},
	{"Protherm", []string{"protherm", "медведь klz", "медведь klom", "медведь plo", "пантера", "гепард", "протерм", "бисон", "bison", "pensotti"}},
	{"Viessmann", []string{"viessmann", "vitopend", "vitodens", "vitogas", "vitola"}},
	{"Buderus", []string{"buderus", "logamax", "logano"}},
	{"Bosch", []string{"bosch", "gaz 6000", "gaz 4000", "condens 2500", "condens 7000"}},
	{"Navien", []string{"navien", "deluxe s", "deluxe c", "deluxe one", "smart tok", "navien ace"}},
	{"Ferroli", []string{"ferroli", "divabel", "divatop", "domiproject", "pegasus", "fortuna"}},
	{"Ariston", []string{"ariston", "cares x", "genus x", "clas x", "alteas"}},
	{"De Dietrich", []string{"de dietrich", "nanéo", "naneo", "dtg 130"}},
	{"Wolf", []string{"wolf gmbh", "wolf cgb", "wolf fgg", "wolf cgg"}},
	{"Immergas", []string{"immergas", "eolo star", "eolo mythos", "nike star", "victrix"}},
	{"Kiturami", []string{"kiturami", "world alpha", "stso"}},
	{"Rinnai", []string{"rinnai"}},
	{"Mizudo", []string{"mizudo"}},
	{"Лемакс", []string{"лемакс", "lemax", "clever", "wise"}},
	{"Fondital", []string{"fondital", "antea", "formentera", "victoria compact"}},
	{"Beretta", []string{"beretta", "ciao", "mynute", "novella"}},
	{"Thermona", []string{"thermona"}},
	{"ZOTA", []string{"zota", "mk-s", "lux-x"}},
}

var reVaillantSeries = regexp.MustCompile(`(?i)\b(?:vk|vks|vsc|vkk|vu|vuw)\s+(?:int\s+)?\d{2,4}`)

var (
	reBlueprintDrawingCode = regexp.MustCompile(`\b\d{2}\s*-\s*\d{2}\s*-\s*\d{3}(?:\.\d{2})?\b`)
	reAssemblyHeading      = regexp.MustCompile(`^(\d{2}[a-zA-Z]?)\s+([А-ЯЁа-яёA-Za-z].{2,100})$`)
	rePageFooter           = regexp.MustCompile(`^\d+\s*/\s*\d+$`)
	rePositionToken        = regexp.MustCompile(`^(?:\d{1,3}[a-zA-Z]?|[A-Z]\d{1,2})$`)
	rePartNumberToken      = regexp.MustCompile(`^(?:-|[0-9]{5,14}|[A-Za-z0-9]{2,6}[-./][A-Za-z0-9\-./]{2,16})$`)
	reReplacedBy           = regexp.MustCompile(`(?i)(?:замен[её]н\p{L}*|земеня\p{L}*|заменя\p{L}*|replaced\s+by)\s*(?:на\s*)?([0-9A-Za-z][0-9A-Za-z\-./]{4,24})`)
	reMultiSpace           = regexp.MustCompile(`\s{2,}`)
	reYearRangeSuffix      = regexp.MustCompile(`\s+(?:19|20)\d{2}(?:\s*-\s*(?:19|20)\d{2})?$`)
	reCalloutOnlyLine      = regexp.MustCompile(`^\d{1,3}[a-zA-Z]?(?:\s+\d{1,3}[a-zA-Z]?){0,3}$`)
	reMultiColPartEntry    = regexp.MustCompile(`(?:^|\s)(?:(\d{1,2}|X)\s+)?(00\d{8})\s+`)
)

// normalizeBlueprintText приводит неразрывные пробелы и типографские дефисы к стандартным символам.
func normalizeBlueprintText(s string) string {
	r := strings.NewReplacer(
		"\u00a0", " ",
		"\u202f", " ",
		"\u2009", " ",
		"\u2010", "-",
		"\u2011", "-",
		"\u2012", "-",
		"\u2013", "-",
		"\u2014", "-",
	)
	return r.Replace(s)
}

// DetectBoilerBrand определяет производителя котла по имени файла и тексту первых страниц.
func DetectBoilerBrand(filename string, pages []string) string {
	var sample strings.Builder
	sample.WriteString(strings.ToLower(normalizeBlueprintText(filename)))
	sample.WriteByte('\n')
	limit := len(pages)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		sample.WriteString(strings.ToLower(normalizeBlueprintText(pages[i])))
		sample.WriteByte('\n')
	}
	haystack := sample.String()
	for _, b := range knownBoilerBrands {
		for _, pat := range b.patterns {
			if strings.Contains(haystack, pat) {
				return b.canonical
			}
		}
	}
	if reVaillantSeries.MatchString(haystack) {
		return "Vaillant"
	}
	return ""
}

// DetectBoilerModel извлекает основное обозначение модели котла из титульной страницы или имени файла.
func DetectBoilerModel(filename string, pages []string) string {
	if len(pages) > 0 {
		first := normalizeBlueprintText(pages[0])
		lines := strings.Split(first, "\n")
		for idx, rawLine := range lines {
			if idx > 6 {
				break
			}
			line := strings.TrimSpace(rawLine)
			if line == "" || len([]rune(line)) < 3 || len([]rune(line)) > 64 {
				continue
			}
			low := strings.ToLower(line)
			if low == "содержание" || low == "обзор узлов" || strings.HasPrefix(low, "каталог ") ||
				strings.HasPrefix(low, "газовый ") || strings.HasPrefix(low, "напольный ") {
				continue
			}
			// Типовой заголовок модели на чертежах (например: "VK INT 246", "VK 36/4-1 XE", "VSC 266/4-5 200").
			if hasLetterAndDigit(line) && !reAssemblyHeading.MatchString(line) {
				return line
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	base = strings.TrimSpace(normalizeBlueprintText(base))
	base = reYearRangeSuffix.ReplaceAllString(base, "")
	return strings.TrimSpace(base)
}

func hasLetterAndDigit(s string) bool {
	hasL, hasD := false, false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasL = true
		}
		if unicode.IsDigit(r) {
			hasD = true
		}
	}
	return hasL && hasD
}

// isPartsTableHeaderLine определяет строку заголовка таблицы запчастей на странице чертежа.
func isPartsTableHeaderLine(line string) bool {
	low := strings.ToLower(normalizeBlueprintText(line))
	if (strings.Contains(low, "поз.") || strings.Contains(low, "позиц")) &&
		(strings.Contains(low, "material no") || strings.Contains(low, "артикул") || strings.Contains(low, "код") || strings.Contains(low, "описание")) {
		return true
	}
	if strings.Contains(low, "pos.") &&
		(strings.Contains(low, "material no") || strings.Contains(low, "part no") || strings.Contains(low, "art.")) {
		return true
	}
	// Формат каталогов Protherm: "№ | № для заказа | Название" (иногда "заказа" перенесено на следующую строку).
	if strings.Contains(low, "№") && (strings.Contains(low, "для заказа") || strings.Contains(low, "№ для") || strings.Contains(low, "название")) {
		return true
	}
	return false
}

// isBlueprintDiagramPage определяет страницу со взрыв-схемой, монтажной схемой или чертежом котла.
func isBlueprintDiagramPage(pageText, nextPageText string) bool {
	norm := normalizeBlueprintText(pageText)
	low := strings.ToLower(norm)
	if low == "" {
		return false
	}
	// Страница с таблицей запчастей сама по себе не является взрыв-схемой.
	if pageHasPartsTable(norm) {
		return false
	}
	// Явные признаки чертежа / взрыв-схемы / схемы обвязки.
	blueprintPhrases := []string{
		"обзор узлов",
		"схемы монтажа",
		"схема монтажа",
		"пример обвязки",
		"внутренний вид котла",
		"габаритные размеры",
		"взрыв-схема",
		"exploded view",
	}
	for _, p := range blueprintPhrases {
		if strings.Contains(low, p) {
			return true
		}
	}
	// Код чертежа на странице (например, Vaillant "03 - 04 - 076.01").
	if reBlueprintDrawingCode.MatchString(norm) {
		return true
	}
	// Страница с коротким текстом, за которой сразу идёт таблица запчастей (взрыв-схемы Vaillant, Protherm, BAXI и др.).
	if nextPageText != "" && pageHasPartsTable(nextPageText) && len([]rune(norm)) < 600 {
		return true
	}
	// Страница с россыпью выносок позиций (6+ коротких строк из чисел) — характерный признак векторной взрыв-схемы.
	if len([]rune(norm)) < 600 {
		calloutLines := 0
		for _, raw := range strings.Split(norm, "\n") {
			t := strings.TrimSpace(raw)
			if t != "" && reCalloutOnlyLine.MatchString(t) {
				calloutLines++
			}
		}
		if calloutLines >= 6 {
			return true
		}
	}
	return false
}

func pageHasPartsTable(pageText string) bool {
	for _, line := range strings.Split(pageText, "\n") {
		if isPartsTableHeaderLine(line) {
			return true
		}
	}
	return false
}

func extractAssemblyHeading(pageText string) string {
	lines := strings.Split(normalizeBlueprintText(pageText), "\n")
	for idx, raw := range lines {
		if idx > 8 {
			break
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Пропускаем строки оглавления с многоточием ("04 Горелка ........ 4").
		if strings.Contains(line, "....") {
			continue
		}
		if reAssemblyHeading.MatchString(line) {
			return line
		}
	}
	return ""
}

// ParseBoilerBlueprint извлекает бренд, модель и таблицу запчастей из постраничного текста PDF.
func ParseBoilerBlueprint(filename string, pages []string, schematicsByPage map[int][]Attachment) BlueprintMetadata {
	brand := DetectBoilerBrand(filename, pages)
	model := DetectBoilerModel(filename, pages)

	meta := BlueprintMetadata{
		Brand: brand,
		Model: model,
	}

	// Запоминаем страницы взрыв-схем по имени узла, чтобы привязать каждую запчасть к чертежу.
	diagramPageByAssembly := map[string]int{}
	lastDiagramPage := 0

	for idx, rawPage := range pages {
		pageNum := idx + 1
		normPage := normalizeBlueprintText(rawPage)
		assembly := extractAssemblyHeading(normPage)
		nextPage := ""
		if idx+1 < len(pages) {
			nextPage = pages[idx+1]
		}

		if !pageHasPartsTable(normPage) {
			if isBlueprintDiagramPage(normPage, nextPage) {
				lastDiagramPage = pageNum
				if assembly != "" {
					diagramPageByAssembly[strings.ToLower(assembly)] = pageNum
				}
			}
			continue
		}

		meta.IsSpec = true
		diagPage := 0
		if assembly != "" {
			if p, ok := diagramPageByAssembly[strings.ToLower(assembly)]; ok {
				diagPage = p
			}
		}
		if diagPage == 0 && lastDiagramPage > 0 && pageNum-lastDiagramPage <= 3 {
			diagPage = lastDiagramPage
		}

		var diagAttID int64
		if diagPage > 0 && len(schematicsByPage[diagPage]) > 0 {
			diagAttID = schematicsByPage[diagPage][0].ID
		}

		pageParts := parsePartsTablePage(normPage, brand, model, assembly, pageNum, diagPage, diagAttID)
		meta.Parts = append(meta.Parts, pageParts...)
	}

	return meta
}

func parsePartsTablePage(
	pageText, brand, model, assembly string,
	pageNum, diagPage int,
	diagAttID int64,
) []BoilerPart {
	// Если на странице многоколоночная таблица Protherm ("№ | № для заказа | Название" x2/x3),
	// разбираем её специализированным многоколоночным парсером.
	if isMultiColumnPartsPage(pageText) {
		if mcParts := parseMultiColumnPartsPage(pageText, brand, model, assembly, pageNum, diagPage, diagAttID); len(mcParts) > 0 {
			return mcParts
		}
	}

	lines := strings.Split(pageText, "\n")
	headerIdx := -1
	var descColStart, noteColStart int

	for i, raw := range lines {
		if isPartsTableHeaderLine(raw) {
			headerIdx = i
			low := strings.ToLower(raw)
			if pos := strings.Index(low, "описание"); pos > 0 {
				descColStart = runeOffset(raw, pos)
			} else if pos := strings.Index(low, "наименование"); pos > 0 {
				descColStart = runeOffset(raw, pos)
			} else if pos := strings.Index(low, "description"); pos > 0 {
				descColStart = runeOffset(raw, pos)
			}
			if pos := strings.Index(low, "пометка"); pos > 0 {
				noteColStart = runeOffset(raw, pos)
			} else if pos := strings.Index(low, "примечан"); pos > 0 {
				noteColStart = runeOffset(raw, pos)
			} else if pos := strings.Index(low, "note"); pos > 0 {
				noteColStart = runeOffset(raw, pos)
			}
			break
		}
	}
	if headerIdx < 0 {
		return nil
	}

	var parts []BoilerPart
	for i := headerIdx + 1; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || rePageFooter.MatchString(trimmed) {
			continue
		}

		if p, ok := parsePartStartLine(raw, descColStart, noteColStart); ok {
			p.Brand = brand
			p.BoilerModel = model
			p.Assembly = assembly
			p.PageNumber = pageNum
			p.DiagramPage = diagPage
			p.DiagramAttachmentID = diagAttID
			parts = append(parts, p)
			continue
		}

		// Многострочное продолжение описания или заметки предыдущей строки таблицы.
		if len(parts) > 0 {
			appendPartContinuation(&parts[len(parts)-1], raw, descColStart, noteColStart)
		}
	}

	for i := range parts {
		parts[i].Name = cleanInlineSpaces(parts[i].Name)
		parts[i].Notes = cleanInlineSpaces(parts[i].Notes)
		if parts[i].ReplacedBy == "" && parts[i].Notes != "" {
			if m := reReplacedBy.FindStringSubmatch(parts[i].Notes); len(m) == 2 {
				parts[i].ReplacedBy = strings.TrimRight(m[1], ".,;)")
			}
		}
	}
	return parts
}

func runeOffset(s string, byteIdx int) int {
	if byteIdx <= 0 {
		return 0
	}
	if byteIdx > len(s) {
		byteIdx = len(s)
	}
	return len([]rune(s[:byteIdx]))
}

func parsePartStartLine(raw string, descColStart, noteColStart int) (BoilerPart, bool) {
	trimmed := strings.TrimSpace(raw)
	fields := strings.Fields(trimmed)
	if len(fields) < 3 {
		return BoilerPart{}, false
	}
	if !rePositionToken.MatchString(fields[0]) || !rePartNumberToken.MatchString(fields[1]) {
		return BoilerPart{}, false
	}

	pos := fields[0]
	partNo := fields[1]
	if partNo == "-" {
		partNo = ""
	}

	// Если строка в layout-формате (с выравниванием колонок пробелами), делим описание и заметку по координатам колонок.
	runes := []rune(raw)
	if noteColStart > 0 && len(runes) > noteColStart-2 {
		// Находим начало описания после partNo.
		startDesc := descColStart
		if startDesc <= 0 {
			idx := strings.Index(raw, fields[1])
			if idx >= 0 {
				startDesc = runeOffset(raw, idx+len(fields[1]))
			}
		}
		// Убедимся, что граница заметки приходится на пробел (не режем слово посередине).
		splitAt := adjustColumnSplit(runes, noteColStart-2, startDesc)
		namePart := ""
		notePart := ""
		if startDesc < len(runes) {
			if splitAt > startDesc && splitAt < len(runes) {
				namePart = strings.TrimSpace(string(runes[startDesc:splitAt]))
				notePart = strings.TrimSpace(string(runes[splitAt:]))
			} else {
				namePart = strings.TrimSpace(string(runes[startDesc:]))
			}
		}
		if namePart == "-" {
			namePart = ""
		}
		if namePart != "" || notePart != "" {
			return BoilerPart{
				Position:   pos,
				PartNumber: partNo,
				Name:       namePart,
				Notes:      notePart,
			}, true
		}
	}

	// Резервный разбор: делим по двойным пробелам или просто берём хвост строки как описание + известные маркеры заметок.
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, fields[1]))
	cols := reMultiSpace.Split(rest, 2)
	namePart := strings.TrimSpace(cols[0])
	notePart := ""
	if len(cols) > 1 {
		notePart = strings.TrimSpace(cols[1])
	} else {
		namePart, notePart = splitInlineNote(namePart)
	}
	if namePart == "-" {
		namePart = ""
	}
	return BoilerPart{
		Position:   pos,
		PartNumber: partNo,
		Name:       namePart,
		Notes:      notePart,
	}, true
}

func adjustColumnSplit(runes []rune, col, minCol int) int {
	if col <= minCol || col >= len(runes) {
		return col
	}
	if runes[col] == ' ' || runes[col] == '\t' {
		return col
	}
	// Ищем ближайший разрыв из 2+ пробелов слева или справа в окне +-6 символов.
	for delta := 1; delta <= 6; delta++ {
		if col-delta > minCol && runes[col-delta] == ' ' {
			return col - delta
		}
		if col+delta < len(runes) && runes[col+delta] == ' ' {
			return col + delta
		}
	}
	return col
}

func appendPartContinuation(p *BoilerPart, raw string, descColStart, noteColStart int) {
	runes := []rune(raw)
	if noteColStart > 0 && len(runes) > noteColStart-2 {
		leadSpaces := 0
		for leadSpaces < len(runes) && runes[leadSpaces] == ' ' {
			leadSpaces++
		}
		if leadSpaces >= noteColStart-4 {
			p.Notes = joinPartText(p.Notes, strings.TrimSpace(raw))
			return
		}
		splitAt := adjustColumnSplit(runes, noteColStart-2, descColStart)
		left := strings.TrimSpace(string(runes[:splitAt]))
		right := strings.TrimSpace(string(runes[splitAt:]))
		if left != "" {
			p.Name = joinPartText(p.Name, left)
		}
		if right != "" {
			p.Notes = joinPartText(p.Notes, right)
		}
		return
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return
	}
	low := strings.ToLower(trimmed)
	if strings.HasPrefix(low, "с поз.") || strings.HasPrefix(low, "не показан") ||
		strings.HasPrefix(low, "заменен") || strings.HasPrefix(low, "земеня") ||
		strings.HasPrefix(low, "недоступно") || strings.HasPrefix(low, "более не доступно") ||
		strings.HasPrefix(low, "может быть") || strings.HasPrefix(low, "может применяться") {
		p.Notes = joinPartText(p.Notes, trimmed)
		return
	}
	p.Name = joinPartText(p.Name, trimmed)
}

func splitInlineNote(s string) (string, string) {
	markers := []string{
		" не показан",
		" Не показан",
		" с поз.",
		" заменен на",
		" Заменен на",
		" земеняется на",
		" недоступно как запчасть",
		" более не доступно",
	}
	for _, m := range markers {
		if idx := strings.Index(s, m); idx > 0 {
			return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx:])
		}
	}
	return s, ""
}

func joinPartText(base, add string) string {
	add = strings.TrimSpace(add)
	if add == "" {
		return base
	}
	if base == "" {
		return add
	}
	if strings.HasSuffix(base, "-") {
		return base + add
	}
	return base + " " + add
}

func cleanInlineSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func isMultiColumnPartsPage(pageText string) bool {
	for _, raw := range strings.Split(pageText, "\n") {
		low := strings.ToLower(raw)
		if strings.Count(low, "название") >= 2 || strings.Count(low, "№ для") >= 2 {
			return true
		}
	}
	return false
}

// parseMultiColumnPartsPage разбирает 2- и 3-колоночные таблицы запчастей (например, каталоги Protherm),
// где в одной строке идут несколько пар "(№) (00200xxxxx) (Название)".
func parseMultiColumnPartsPage(
	pageText, brand, defaultModel, assembly string,
	pageNum, diagPage int,
	diagAttID int64,
) []BoilerPart {
	lines := strings.Split(pageText, "\n")
	pageModel := detectPageModelHeading(lines, defaultModel)

	var parts []BoilerPart
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || isPartsTableHeaderLine(line) {
			continue
		}
		matches := reMultiColPartEntry.FindAllStringSubmatchIndex(line, -1)
		if len(matches) == 0 {
			continue
		}
		for i, m := range matches {
			pos := ""
			if m[2] >= 0 && m[3] > m[2] {
				pos = strings.TrimSpace(line[m[2]:m[3]])
			}
			partNo := strings.TrimSpace(line[m[4]:m[5]])
			nameStart := m[1]
			nameEnd := len(line)
			if i+1 < len(matches) {
				nameEnd = matches[i+1][0]
			}
			name := cleanInlineSpaces(line[nameStart:nameEnd])
			if partNo == "" || name == "" {
				continue
			}
			parts = append(parts, BoilerPart{
				Brand:               brand,
				BoilerModel:         pageModel,
				Assembly:            assembly,
				Position:            pos,
				PartNumber:          partNo,
				Name:                name,
				PageNumber:          pageNum,
				DiagramPage:         diagPage,
				DiagramAttachmentID: diagAttID,
			})
		}
	}
	return parts
}

func detectPageModelHeading(lines []string, fallback string) string {
	for _, raw := range lines {
		t := strings.TrimSpace(raw)
		if t == "" || len([]rune(t)) < 5 || len([]rune(t)) > 80 {
			continue
		}
		low := strings.ToLower(t)
		if strings.HasPrefix(low, "напольные котлы") || strings.Contains(low, "№ для") ||
			strings.Contains(low, "название") || reCalloutOnlyLine.MatchString(t) {
			continue
		}
		if reMultiColPartEntry.MatchString(t) {
			continue
		}
		if strings.Contains(low, "bison") || strings.Contains(low, "pensotti") ||
			strings.Contains(low, "горелка") || strings.Contains(low, "nl") || strings.Contains(low, "no ") {
			return t
		}
	}
	return fallback
}

// ExpandFaultKeywords сопоставляет типовые симптомы и коды неисправностей котлов с узлами и запчастями на чертежах.
func ExpandFaultKeywords(symptom string) []string {
	low := strings.ToLower(normalizeBlueprintText(symptom))
	var out []string
	seen := map[string]bool{}
	add := func(words ...string) {
		for _, w := range words {
			if !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}

	switch {
	case containsAny(low, "розжиг", "не разжиг", "плам", "ионизац", "f.28", "f28", "f.29", "f29", "e01", "e1", "газ"):
		add("электрод", "розжиг", "газовая арматура", "газовый клапан", "кабель розжига", "горелка", "плата управления", "вентилятор")
	case containsAny(low, "давлен", "манометр", "подпитк", "f.22", "f22", "f.75", "f75", "e10", "теч", "протеч", "капает"):
		add("датчик давления", "манометр", "предохранительный клапан", "насос", "прокладка", "кран", "воздухоотводчик")
	case containsAny(low, "перегрев", "температур", "ntc", "stb", "f.20", "f20", "f.23", "f23", "f.24", "f24", "e02", "e2", "кипит"):
		add("ntc", "датчик", "теплообменник", "насос", "термостат", "stb", "кабельный жгут")
	case containsAny(low, "дымоход", "тяг", "вентилятор", "прессостат", "f.32", "f32", "f.33", "f33", "e03", "e3", "воздух"):
		add("вентилятор", "воздуховод", "реле давления", "прессостат", "адаптер", "датчик отходящих газов", "камера")
	case containsAny(low, "конденсат", "сифон", "слив"):
		add("сифон", "конденсат", "шланг", "прокладка")
	case containsAny(low, "не включа", "диспле", "экран", "плат", "предохранител", "питан", "ошибк"):
		add("плата управления", "предохранитель", "дисплей", "кабельный жгут", "штекер")
	case containsAny(low, "гвс", "горяч", "бойлер", "водонагревател", "трехходовой", "трёхходовой"):
		add("водонагреватель", "теплообменник", "датчик", "клапан", "насос")
	}
	return out
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
