package docs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

func TestParseBoilerBlueprintVaillantLayout(t *testing.T) {
	pages := []string{
		"VK\u00a0INT\u00a0246\nГазовый напольный котел | Газовый конденсационный котел VK/VKS ecoVIT plus\nСодержание\nОбзор узлов ..... 2\n04 Горелка ..... 4",
		"Обзор узлов\nVK INT 246\n03 - 00 - 080.01\n2 / 17",
		"Обзор узлов\nПоз. Группа узлов\n04 Горелка",
		"04 Горелка\nVK INT 246\n04 Горелка\n03 - 04 - 076.01\n4 / 17",
		strings.Join([]string{
			"04 Горелка",
			"VK INT 246",
			"Поз.  Material No.  Описание                          Пометка, заметка",
			"01    049345        Горелка                           VK INT 246, с поз.02-03",
			"06    090709        Электрод                          с поз. 07, 08",
			"09    0020038588    Кабель розжига",
			"13    053471        Газовая арматура                  VK INT 246, с поз.11, 14, 26",
			"18    0020010867    Фланец горелки, с                 с поз.02-05, 07-08, 12, 14, 17-21",
			"                    коллектором",
			"5 / 17",
		}, "\n"),
		"08 Гидравлические части\nVK INT 246\n03 - 08 - 034.01\n13 / 17",
		strings.Join([]string{
			"08 Гидравлические части",
			"VK INT 246",
			"Поз.  Material No.  Описание                          Пометка, заметка",
			"29    0020059717    Датчик давления воды              с поз.27-28",
			"38    104521        Воздухоотводчик                   земеняется на 0010033857, может быть использован",
			"44    -             -                                 более не доступно для заказа, см. поз. 43",
			"15 / 17",
		}, "\n"),
	}

	schematicsByPage := map[int][]Attachment{
		4: {{ID: 101, Filename: "schematic_page_04.png", PageNumber: 4}},
		6: {{ID: 102, Filename: "schematic_page_06.png", PageNumber: 6}},
	}

	meta := ParseBoilerBlueprint("VK INT 246 2008-2012.pdf", pages, schematicsByPage)
	if meta.Brand != "Vaillant" {
		t.Fatalf("Brand = %q, want Vaillant", meta.Brand)
	}
	if meta.Model != "VK INT 246" {
		t.Fatalf("Model = %q, want VK INT 246", meta.Model)
	}
	if len(meta.Parts) != 8 {
		t.Fatalf("len(Parts) = %d, want 8: %+v", len(meta.Parts), meta.Parts)
	}

	// Проверяем многострочное описание и привязку к чертежу узла.
	flange := meta.Parts[4]
	if flange.Position != "18" || flange.PartNumber != "0020010867" {
		t.Fatalf("unexpected flange part: %+v", flange)
	}
	if !strings.Contains(flange.Name, "Фланец горелки") || !strings.Contains(flange.Name, "коллектором") {
		t.Fatalf("multiline description not merged: %q", flange.Name)
	}
	if flange.DiagramPage != 4 || flange.DiagramAttachmentID != 101 {
		t.Fatalf("diagram link mismatch: page=%d att=%d", flange.DiagramPage, flange.DiagramAttachmentID)
	}

	// Проверяем извлечение замены артикула.
	vent := meta.Parts[6]
	if vent.PartNumber != "104521" || vent.ReplacedBy != "0010033857" {
		t.Fatalf("replacement not extracted: %+v", vent)
	}
	if vent.DiagramPage != 6 || vent.DiagramAttachmentID != 102 {
		t.Fatalf("vent diagram link mismatch: page=%d att=%d", vent.DiagramPage, vent.DiagramAttachmentID)
	}

	// Проверяем позицию без артикула ("-").
	unavail := meta.Parts[7]
	if unavail.Position != "44" || unavail.PartNumber != "" || !strings.Contains(unavail.Notes, "более не доступно") {
		t.Fatalf("unavailable part parsed incorrectly: %+v", unavail)
	}
}

func TestDetectBoilerBrandNoFalseNavienOnReplaced(t *testing.T) {
	pages := []string{
		"VK 36/4-1 XE\nГазовый напольный котел | Напольный газовый котёл VK (6-76 кВт)\nСодержание",
		"04a Горелка\n18 139239 Винт replaced by 0010047517, can be consumed",
	}
	if got := DetectBoilerBrand("VK 364-1 XE 1994-1996.pdf", pages); got != "Vaillant" {
		t.Fatalf("DetectBoilerBrand() = %q, want Vaillant", got)
	}
}

func TestParseProthermMultiColumnAndSanitizeMarkdown(t *testing.T) {
	pages := []string{
		"КАТАЛОГ ЗАПАСНЫХ ЧАСТЕЙ\nНапольные котлы",
		"18,24,35,50 NL - Pensotti\n26\n24\n25\n23\n22\n20\n19\n18\nНапольные котлы 2e.1",
		strings.Join([]string{
			"№  № для заказа  Название  №  № для заказа  Название  №  № для заказа  Название",
			"4  0020033843  Фитинг NL  18  0020033240  Клеммник 6336-37-12-10  28  0020033221  Изолирующая втулка 9 мм",
			"5  0020033327  Воздухоотводчик 1/4\"  19  0020033553  Панель блока управл.NL  29  0020043361  Панель облицов.(прав.)30NL",
			"18,24,35,50 NL - Pensotti",
		}, "\n"),
	}

	if !isBlueprintDiagramPage(pages[1], pages[2]) {
		t.Fatalf("expected Protherm exploded view page to be detected as blueprint diagram")
	}

	schematicsByPage := map[int][]Attachment{
		2: {{ID: 201, Filename: "schematic_page_02.png", PageNumber: 2}},
	}
	meta := ParseBoilerBlueprint("Protherm_Floor.pdf", pages, schematicsByPage)
	if meta.Brand != "Protherm" {
		t.Fatalf("Brand = %q, want Protherm", meta.Brand)
	}
	if len(meta.Parts) != 6 {
		t.Fatalf("len(Parts) = %d, want 6: %+v", len(meta.Parts), meta.Parts)
	}
	if meta.Parts[1].PartNumber != "0020033240" || !strings.Contains(meta.Parts[1].Name, "Клеммник") || meta.Parts[1].DiagramAttachmentID != 201 {
		t.Fatalf("unexpected parsed multi-col part: %+v", meta.Parts[1])
	}

	// Проверяем, что ведущие пробелы (вызывающие чёрные <pre>-блоки в Markdown) и длинные цепочки точек убираются.
	md := sanitizePdfPageForMarkdown("      26\n        04a Горелка ................................................ 4\n")
	if strings.HasPrefix(md, " ") || strings.Contains(md, "\n    ") {
		t.Fatalf("leading spaces not stripped (would render as <pre>): %q", md)
	}
	if strings.Contains(md, "......") {
		t.Fatalf("long dot leader not compacted: %q", md)
	}
}

func TestIsBlueprintDiagramPage(t *testing.T) {
	cases := []struct {
		name     string
		page     string
		nextPage string
		want     bool
	}{
		{
			name: "vaillant exploded drawing code",
			page: "04 Горелка\nVK INT 246\n03 - 04 - 076.01\n4 / 17",
			want: true,
		},
		{
			name:     "vaillant diagram page before parts table",
			page:     "06 Теплообменник\nVK INT 246\n7 / 17",
			nextPage: "06 Теплообменник\nПоз. Material No. Описание Пометка, заметка\n01 0020135131 Теплообменник",
			want:     true,
		},
		{
			name: "kentatsu installation diagram",
			page: "46\nСхемы монтажа конденсационных котлов\nНастенные Конденсационные газовые котлы",
			want: true,
		},
		{
			name: "kentatsu piping example",
			page: "82\nПример обвязки котла\n1 - Сбросной клапан на 3 бара",
			want: true,
		},
		{
			name: "vaillant parts table is not a diagram",
			page: "04 Горелка\nПоз. Material No. Описание Пометка, заметка\n01 049345 Горелка",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBlueprintDiagramPage(tc.page, tc.nextPage); got != tc.want {
				t.Fatalf("isBlueprintDiagramPage() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBoilerToolsDiagnoseAndHTTP(t *testing.T) {
	now := time.Date(2026, 11, 23, 23, 0, 0, 0, time.UTC)
	repo := newMemDocsRepo(func() time.Time { return now })
	mod := NewModule(repo, nil, fakeEntityResolver{}, nil, t.TempDir(), nil)
	ctx := context.Background()
	admin := &platform.AuthContext{
		UserID:       1,
		Username:     "admin",
		IsSuperAdmin: true,
		Permissions:  map[string]bool{"docs.view": true, "docs.create": true, "docs.update": true},
	}

	doc, err := mod.Service.CreateDocument(ctx, admin, SaveDocumentInput{
		Space:   "equipment",
		DocType: DocTypeEquipmentManual,
		Title:   "Vaillant VK INT 246 (взрыв-схема)",
		Content: "Газовый конденсационный котел VK INT 246 ecoVIT plus",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	att, err := mod.Service.saveAttachmentWithMeta(ctx, admin, doc.ID, "schematic_page_04.png", "image/png", makeTestPNG(t, 40, 40), AttachmentKindSchematic, 4, AnalysisStatusDone)
	if err != nil {
		t.Fatalf("save attachment: %v", err)
	}

	err = repo.ReplaceBoilerParts(ctx, doc.ID, []BoilerPart{
		{
			Brand:               "Vaillant",
			BoilerModel:         "VK INT 246",
			Assembly:            "04 Горелка",
			Position:            "06",
			PartNumber:          "090709",
			Name:                "Электрод",
			Notes:               "с поз. 07, 08",
			PageNumber:          5,
			DiagramPage:         4,
			DiagramAttachmentID: att.ID,
		},
		{
			Brand:               "Vaillant",
			BoilerModel:         "VK INT 246",
			Assembly:            "04 Горелка",
			Position:            "13",
			PartNumber:          "053471",
			Name:                "Газовая арматура",
			Notes:               "VK INT 246, с поз.11, 14, 26",
			PageNumber:          5,
			DiagramPage:         4,
			DiagramAttachmentID: att.ID,
		},
		{
			Brand:       "Vaillant",
			BoilerModel: "VK INT 246",
			Assembly:    "08 Гидравлические части",
			Position:    "38",
			PartNumber:  "104521",
			Name:        "Воздухоотводчик",
			Notes:       "земеняется на 0010033857",
			ReplacedBy:  "0010033857",
			PageNumber:  15,
			DiagramPage: 13,
		},
	})
	if err != nil {
		t.Fatalf("replace boiler parts: %v", err)
	}

	// 1. Проверяем инструмент чата docs_search_boiler_parts.
	var searchTool, diagnoseTool *platform.ToolDefinition
	for i := range mod.Tools() {
		tl := mod.Tools()[i]
		if tl.Name == "docs_search_boiler_parts" {
			searchTool = &tl
		}
		if tl.Name == "docs_diagnose_boiler" {
			diagnoseTool = &tl
		}
	}
	if searchTool == nil || diagnoseTool == nil {
		t.Fatalf("expected boiler tools to be registered")
	}

	res, err := searchTool.Execute(ctx, admin, json.RawMessage(`{"model":"VK INT 246","part_number":"104521"}`))
	if err != nil {
		t.Fatalf("docs_search_boiler_parts error: %v", err)
	}
	if !strings.Contains(res.Content, "0010033857") || !strings.Contains(res.Content, "Воздухоотводчик") {
		t.Fatalf("unexpected search tool output: %s", res.Content)
	}

	// 2. Проверяем инструмент чата docs_diagnose_boiler по симптому "не разжигается".
	diagRes, err := diagnoseTool.Execute(ctx, admin, json.RawMessage(`{"model":"VK INT 246","symptom":"не разжигается котел"}`))
	if err != nil {
		t.Fatalf("docs_diagnose_boiler error: %v", err)
	}
	if !strings.Contains(diagRes.Content, "090709") || !strings.Contains(diagRes.Content, "053471") {
		t.Fatalf("diagnose tool should include electrode and gas valve: %s", diagRes.Content)
	}
	if !strings.Contains(diagRes.Content, "schematic_page_04.png") {
		t.Fatalf("diagnose tool should include schematic attachment: %s", diagRes.Content)
	}

	// 3. Проверяем индексацию в единый поиск (boilerPartsIndexSource).
	var partsSource platform.IndexSource
	for _, src := range mod.SearchSources() {
		if src.SourceType() == "boiler_parts" {
			partsSource = src
			break
		}
	}
	if partsSource == nil {
		t.Fatalf("boiler_parts IndexSource not registered")
	}
	items, err := partsSource.ListForIndex(ctx, nil)
	if err != nil || len(items) != 2 {
		t.Fatalf("ListForIndex() = %d, err=%v, want 2 assembly groups", len(items), err)
	}

	// 4. Проверяем HTTP API /api/v1/docs/boiler-parts.
	r := chi.NewRouter()
	h := NewHandler(mod.Service, staticAuth{ac: admin}, nil)
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/docs/boiler-parts?model=VK+INT+246&q=электрод", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /boiler-parts status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"090709"`) {
		t.Fatalf("GET /boiler-parts body missing expected part number: %s", rec.Body.String())
	}
}

func TestParseBaxiEnglishCatalogAndSearchSynonyms(t *testing.T) {
	pages := []string{
		"SPARE PARTS CATALOGUE\n- June 2017 -\nBAXI S.p.A.\n",
		"Pos     Code              Description code\n" +
			"154   5702450     PCB GROUP BERT&PART HDIMS 02        •       •       •\n" +
			"Pos     Code              Description code\n" +
			"154   710825300   PCB SM11469                         •       •       1607\n" +
			"169   9951650     MANOMETER D.40                      •       •\n",
	}

	meta := ParseBoilerBlueprint("eco-four-katalog-zapchastey-ot-iyun-2017-_eng.pdf", pages, nil)
	if meta.Brand != "BAXI" {
		t.Fatalf("Brand = %q, want BAXI", meta.Brand)
	}
	if strings.Contains(strings.ToLower(meta.Model), "june") {
		t.Fatalf("Model should skip date-only title line, got %q", meta.Model)
	}
	if len(meta.Parts) != 3 {
		t.Fatalf("len(Parts) = %d, want 3 (parts=%+v)", len(meta.Parts), meta.Parts)
	}
	if meta.Parts[0].PartNumber != "5702450" || meta.Parts[0].Name != "PCB GROUP BERT&PART HDIMS 02" || meta.Parts[0].Notes != "" {
		t.Fatalf("part[0] = %+v", meta.Parts[0])
	}
	if meta.Parts[1].PartNumber != "710825300" || meta.Parts[1].Notes != "1607" {
		t.Fatalf("part[1] = %+v", meta.Parts[1])
	}

	tokens := splitSearchTokens("BAXI — eco-four 24 F")
	wantTokens := []string{"baxi", "eco", "four", "24", "f"}
	if len(tokens) != len(wantTokens) {
		t.Fatalf("splitSearchTokens = %v, want %v", tokens, wantTokens)
	}
	for i := range wantTokens {
		if tokens[i] != wantTokens[i] {
			t.Fatalf("splitSearchTokens[%d] = %q, want %q", i, tokens[i], wantTokens[i])
		}
	}

	syns := ExpandPartQuerySynonyms("плата управления")
	foundPCB := false
	for _, s := range syns {
		if s == "pcb" {
			foundPCB = true
			break
		}
	}
	if !foundPCB {
		t.Fatalf("ExpandPartQuerySynonyms(плата управления) = %v, expected pcb", syns)
	}

	// «трехходовой клапан» должен ставить 3-way первым и НЕ подмешивать gas valve.
	valveSyns := ExpandPartQuerySynonyms("трехходовой клапан")
	if len(valveSyns) == 0 || valveSyns[0] != "3-way" {
		t.Fatalf("ExpandPartQuerySynonyms(трехходовой клапан) = %v, expected 3-way first", valveSyns)
	}
	for _, s := range valveSyns {
		if s == "gas valve" {
			t.Fatalf("ExpandPartQuerySynonyms(трехходовой клапан) should not include gas valve: %v", valveSyns)
		}
	}

	// splitModelSearchTokens должен убирать общие слова «Котел» и «бойлер» из CRM-названий.
	modelTokens := splitModelSearchTokens("Котел бойлер Protherm FE 200 BM БОЙЛЕР")
	for _, tok := range modelTokens {
		if tok == "котел" || tok == "бойлер" {
			t.Fatalf("splitModelSearchTokens kept generic word %q: %v", tok, modelTokens)
		}
	}

	// Проверяем, что SearchBoilerParts находит англоязычный PCB по русскому запросу «плата».
	now := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	repo := newMemDocsRepo(func() time.Time { return now })
	mod := NewModule(repo, nil, fakeEntityResolver{}, nil, t.TempDir(), nil)
	ctx := context.Background()
	admin := &platform.AuthContext{
		UserID:       1,
		Username:     "admin",
		IsSuperAdmin: true,
		Permissions:  map[string]bool{"docs.view": true, "docs.create": true, "docs.update": true},
	}
	doc, err := mod.Service.CreateDocument(ctx, admin, SaveDocumentInput{
		Space:   "equipment",
		DocType: DocTypeEquipmentManual,
		Title:   "BAXI — eco-four-katalog-zapchastey-ot-iyun-2017-_eng",
		Content: "ECOFOUR 24 F",
		Status:  "published",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	if err := repo.ReplaceBoilerParts(ctx, doc.ID, meta.Parts); err != nil {
		t.Fatalf("replace boiler parts: %v", err)
	}
	found, err := mod.Service.SearchBoilerParts(ctx, admin, BoilerPartFilter{
		Brand: "BAXI",
		Query: "плата",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchBoilerParts error: %v", err)
	}
	if len(found) < 2 {
		t.Fatalf("expected >=2 PCB parts for query=плата, got %d (%+v)", len(found), found)
	}
}
