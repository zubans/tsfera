package docs

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

type stubSchematicAnalyzer struct {
	calls     int
	lastOp    string
	lastImgs  int
	lastSys   string
	lastUser  string
	response  string
	returnErr error
}

func (s *stubSchematicAnalyzer) AnalyzeImage(
	_ context.Context,
	_ *platform.AuthContext,
	systemPrompt, userPrompt string,
	images []SchematicImage,
	operation string,
) (string, error) {
	s.calls++
	s.lastOp = operation
	s.lastImgs = len(images)
	s.lastSys = systemPrompt
	s.lastUser = userPrompt
	if s.returnErr != nil {
		return "", s.returnErr
	}
	return s.response, nil
}

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestIsSchematicPageText(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "protherm electrical schematic header",
			text: "Электрическая схема котла PROTHERM 20 (30, 40, 50) KLZ\nплата управления PROTHERM ST6-10",
			want: true,
		},
		{
			name: "english wiring diagram",
			text: "Figure 10. Electrical wiring diagram for boiler control board",
			want: true,
		},
		{
			name: "anchor plus tech terms",
			text: "Рис. 10. Плата управления XT2: насос M1, газовый клапан Honeywell, термостат 95C",
			want: true,
		},
		{
			name: "ordinary manual text",
			text: "Гарантийные обязательства и правила безопасной эксплуатации оборудования.",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSchematicPageText(tc.text); got != tc.want {
				t.Fatalf("isSchematicPageText() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildPdfMarkdownWithSchematics(t *testing.T) {
	pages := []string{"Страница 1: Введение", "Страница 2: Электрическая схема"}
	byPage := map[int][]Attachment{
		2: {{ID: 42, Filename: "schematic_page_02.png", PageNumber: 2}},
	}
	got := buildPdfMarkdownWithSchematics(pages, byPage)
	if !strings.Contains(got, "Страница 1: Введение") {
		t.Fatalf("missing page 1 text: %q", got)
	}
	if !strings.Contains(got, "![Схема (стр. 2): schematic_page_02.png](/api/v1/docs/attachments/42/download?inline=1)") {
		t.Fatalf("missing embedded schematic markdown link: %q", got)
	}
}

func TestCropImageRegion(t *testing.T) {
	raw := makeTestPNG(t, 200, 100)

	cropped, err := cropImageRegion(raw, "top_left")
	if err != nil {
		t.Fatalf("crop top_left: %v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(cropped))
	if err != nil {
		t.Fatalf("decode cropped: %v", err)
	}
	if cfg.Width != 110 || cfg.Height != 55 {
		t.Fatalf("expected 110x55, got %dx%d", cfg.Width, cfg.Height)
	}

	custom, err := cropImageRegion(raw, "0.25,0.5,0.75,1.0")
	if err != nil {
		t.Fatalf("crop custom: %v", err)
	}
	cfg2, _, err := image.DecodeConfig(bytes.NewReader(custom))
	if err != nil {
		t.Fatalf("decode custom: %v", err)
	}
	if cfg2.Width != 100 || cfg2.Height != 50 {
		t.Fatalf("expected 100x50, got %dx%d", cfg2.Width, cfg2.Height)
	}
}

func TestProcessPendingSchematicsAndInspectSchematic(t *testing.T) {
	now := time.Date(2026, 11, 22, 23, 0, 0, 0, time.UTC)
	repo := newMemDocsRepo(func() time.Time { return now })
	bus := platform.NewInMemoryEventBus()
	svc := NewService(repo, fakeEntityResolver{}, bus, t.TempDir(), nil)
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	doc, err := svc.CreateDocument(context.Background(), admin, SaveDocumentInput{
		Space:   "manuals",
		DocType: DocTypeEquipmentManual,
		Title:   "Protherm KLZ",
		Content: "# Protherm KLZ",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}

	pngBytes := makeTestPNG(t, 400, 300)
	att, err := svc.saveAttachmentWithMeta(
		context.Background(), admin, doc.ID, "schematic_page_10.png", "image/png",
		pngBytes, AttachmentKindSchematic, 10, AnalysisStatusPending,
	)
	if err != nil {
		t.Fatalf("save schematic attachment: %v", err)
	}

	// 1. Если Vision-модель не настроена — вложение остаётся в pending.
	notCfg := &stubSchematicAnalyzer{returnErr: platform.NewNotConfigured("не настроена")}
	svc.SetSchematicAnalyzer(notCfg)
	n, err := svc.ProcessPendingSchematics(context.Background(), 5)
	if err != nil || n != 0 {
		t.Fatalf("expected 0 processed when not configured, got n=%d err=%v", n, err)
	}
	afterSkip, _ := repo.GetAttachment(context.Background(), att.ID)
	if afterSkip.AnalysisStatus != AnalysisStatusPending {
		t.Fatalf("expected pending after not_configured, got %q", afterSkip.AnalysisStatus)
	}

	// 2. Успешный фоновый анализ сохраняет паспорт схемы в extracted_text.
	okAnalyzer := &stubSchematicAnalyzer{
		response: "### 1. Название и оборудование\n- PROTHERM 20 KLZ, плата ST6-10",
	}
	svc.SetSchematicAnalyzer(okAnalyzer)
	n, err = svc.ProcessPendingSchematics(context.Background(), 5)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 processed, got n=%d err=%v", n, err)
	}
	if okAnalyzer.lastOp != "schematic_index" || okAnalyzer.lastImgs != 1 {
		t.Fatalf("unexpected index call: op=%q imgs=%d", okAnalyzer.lastOp, okAnalyzer.lastImgs)
	}
	analyzed, _ := repo.GetAttachment(context.Background(), att.ID)
	if analyzed.AnalysisStatus != AnalysisStatusDone || !strings.Contains(analyzed.ExtractedText, "PROTHERM 20 KLZ") {
		t.Fatalf("unexpected analyzed attachment: %+v", analyzed)
	}

	// 3. Инспекция оригинала схемы с кропом области передаёт 2 изображения (общий вид + кроп).
	okAnalyzer.response = "Насос бойлера M2 подключён к клеммнику XT2 [подтверждено на схеме]."
	gotAtt, ans, err := svc.InspectSchematic(context.Background(), admin, att.ID, "Куда подключён насос M2?", "bottom_left")
	if err != nil {
		t.Fatalf("InspectSchematic failed: %v", err)
	}
	if gotAtt.ID != att.ID || okAnalyzer.lastOp != "schematic_inspect" || okAnalyzer.lastImgs != 2 {
		t.Fatalf("unexpected inspect call: att=%d op=%q imgs=%d", gotAtt.ID, okAnalyzer.lastOp, okAnalyzer.lastImgs)
	}
	if !strings.Contains(ans, "M2") {
		t.Fatalf("unexpected inspect answer: %q", ans)
	}
}
