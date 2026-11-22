package docs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// SchematicImage — изображение схемы для передачи в Vision-модель.
type SchematicImage struct {
	MimeType string
	Data     []byte
}

// SchematicAnalyzer — узкий интерфейс анализа изображений/схем (реализуется через ai.Gateway / cli_gateway).
type SchematicAnalyzer interface {
	AnalyzeImage(ctx context.Context, auth *platform.AuthContext, systemPrompt, userPrompt string, images []SchematicImage, operation string) (string, error)
}

// schematicIndexSystemPrompt — жёсткий анти-галлюцинационный промпт для первичной индексации схемы в поиск.
const schematicIndexSystemPrompt = `Ты технический инженер-диагност систем отопления и автоматики. Твоя задача — составить точный структурированный паспорт электрической/гидравлической схемы с изображения для поискового индекса.

ЖЁСТКИЕ ПРАВИЛА ПРОТИВ ГАЛЛЮЦИНАЦИЙ:
1. Описывай ТОЛЬКО то, что реально и однозначно видно на изображении.
2. КАТЕГОРИЧЕСКИ ЗАПРЕЩЕНО додумывать, угадывать или подставлять по памяти модели плат, номера клемм, контактов, цвета проводов и соединения.
3. Если текст, номер контакта или линия неразборчивы или перекрыты — пиши ровно «[нечитаемо]», не придумывай значение.
4. Сохраняй точные обозначения как на схеме (например: PROTHERM ST6-10, Honeywell VK4105G, S4565 AM, XT1, XT2, X12, M1, M2, L, N, PE, PWM1, PWM2).

Выведи результат строго в Markdown по структуре:
### 1. Название и оборудование
- Заголовок схемы и модели оборудования/плат (только если указаны на изображении).

### 2. Блоки и клеммники
- Перечисли все видимые платы, модули, клеммники и разъёмы с их маркировкой.

### 3. Подключённые компоненты и датчики
- Насосы, клапаны, термостаты, датчики, электроды, кнопки, выключатели, таймеры, регуляторы с их точными подписями.

### 4. Подтверждённые соединения и маркировки проводов
- Только однозначно прослеживаемые цепи в формате: Откуда → Куда (провод/цвет/маркировка, промежуточные элементы).

### 5. Нечитаемые или неоднозначные участки
- Укажи, какие клеммы, линии или подписи на изображении неразличимы.`

// schematicInspectSystemPrompt — жёсткий анти-галлюцинационный промпт для точных ответов из чата по оригиналу схемы.
const schematicInspectSystemPrompt = `Ты технический инженер-диагност. Тебе передан оригинал электрической/технической схемы (и при наличии — увеличенный фрагмент интересующей области) и конкретный вопрос пользователя.

СТРОГИЙ КОНТРАКТ (НУЛЕВАЯ ТЕРПИМОСТЬ К ГАЛЛЮЦИНАЦИЯМ):
1. Отвечай ИСКЛЮЧИТЕЛЬНО на основании того, что физически видно на переданном изображении схемы.
2. ЗАПРЕЩЕНО использовать общие знания о котлах/платах, додумывать номера пинов, цвета проводов, номиналы или недостающие связи.
3. Если на схеме НЕТ ответа на вопрос — ответь прямо: «На предоставленной схеме нет данных по этому вопросу.»
4. Если нужный участок (номер контакта, цвет провода, пересечение линий) плохо читается или неоднозначен — ответь прямо: «На схеме этот участок нечитаем / неоднозначен: <что именно не видно>», и НЕ угадывай.
5. Учитывай правила чтения схем: точка (●) на пересечении означает электрическое соединение; пересечение без точки — провода НЕ соединены.
6. Для каждого названного подключения указывай точные обозначения блоков, клеммников, номеров контактов и цветов проводов строго как на схеме, и помечай статус: [подтверждено на схеме] или [нечётко].`

// SetSchematicAnalyzer подключает анализатор схем (шлюз agy через ai.Gateway).
func (s *Service) SetSchematicAnalyzer(a SchematicAnalyzer) {
	s.analyzer = a
}

// ProcessPendingSchematics обрабатывает очередь вложений-схем со статусом pending в фоне.
func (s *Service) ProcessPendingSchematics(ctx context.Context, limit int) (int, error) {
	if s.analyzer == nil {
		return 0, nil
	}
	pending, err := s.repo.ListPendingSchematicAttachments(ctx, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, att := range pending {
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		cleanPath := filepath.Clean(att.StoragePath)
		if !strings.HasPrefix(cleanPath, s.storageDir+string(os.PathSeparator)) {
			_ = s.repo.UpdateAttachmentAnalysis(ctx, att.ID, AnalysisStatusFailed, "", "Недопустимый путь к файлу схемы", nil)
			continue
		}
		imgData, err := os.ReadFile(cleanPath)
		if err != nil {
			_ = s.repo.UpdateAttachmentAnalysis(ctx, att.ID, AnalysisStatusFailed, "", "Файл схемы отсутствует на диске", nil)
			continue
		}

		userPrompt := fmt.Sprintf("Проанализируй схему из файла %s (страница %d) и составь точный паспорт схемы для поиска.", att.Filename, att.PageNumber)
		summary, err := s.analyzer.AnalyzeImage(ctx, nil, schematicIndexSystemPrompt, userPrompt, []SchematicImage{
			{MimeType: att.MimeType, Data: imgData},
		}, "schematic_index")
		if err != nil {
			// Если Vision-модель в шлюзе ещё не настроена — оставляем в pending до настройки.
			if isNotConfiguredError(err) {
				s.logger.Debug("vision model not configured yet, deferring schematic analysis", "attachment_id", att.ID)
				return processed, nil
			}
			s.logger.Warn("schematic background analysis failed", "attachment_id", att.ID, "err", err)
			_ = s.repo.UpdateAttachmentAnalysis(ctx, att.ID, AnalysisStatusFailed, "", err.Error(), nil)
			continue
		}

		now := s.clock()
		if err := s.repo.UpdateAttachmentAnalysis(ctx, att.ID, AnalysisStatusDone, summary, "", &now); err != nil {
			s.logger.Warn("failed to save schematic analysis result", "attachment_id", att.ID, "err", err)
			continue
		}
		s.publishChange(ctx, "docs.attachment.analyzed", att.DocumentID)
		processed++
	}
	return processed, nil
}

// InspectSchematic выполняет точный анализ оригинала схемы (при необходимости с кропом области)
// по конкретному вопросу пользователя из ИИ-чата.
func (s *Service) InspectSchematic(
	ctx context.Context,
	auth *platform.AuthContext,
	attachmentID int64,
	question string,
	region string,
) (*Attachment, string, error) {
	if s.analyzer == nil {
		return nil, "", platform.NewNotConfigured("Анализатор схем не подключён")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, "", platform.NewValidation("Укажите конкретный вопрос по схеме", nil)
	}

	att, imgData, err := s.GetAttachmentForDownload(ctx, auth, attachmentID)
	if err != nil {
		return nil, "", err
	}
	if !strings.HasPrefix(strings.ToLower(att.MimeType), "image/") {
		return nil, "", platform.NewValidation("Указанное вложение не является изображением схемы", nil)
	}

	images := []SchematicImage{{MimeType: att.MimeType, Data: imgData}}
	regionNote := ""
	region = strings.TrimSpace(strings.ToLower(region))
	if region != "" && region != "full" {
		if cropped, cropErr := cropImageRegion(imgData, region); cropErr == nil && len(cropped) > 0 {
			images = append(images, SchematicImage{MimeType: "image/png", Data: cropped})
			regionNote = fmt.Sprintf(" Второе изображение — увеличенный фрагмент области %q для точного чтения мелких обозначений.", region)
		}
	}

	userPrompt := fmt.Sprintf(
		"Схема: %s (вложение #%d, страница %d).%s\n\nВопрос пользователя: %s\n\nОтвечай строго по схеме. Если данных нет или участок нечитаем — прямо сообщи об этом.",
		att.Filename, att.ID, att.PageNumber, regionNote, question,
	)

	answer, err := s.analyzer.AnalyzeImage(ctx, auth, schematicInspectSystemPrompt, userPrompt, images, "schematic_inspect")
	if err != nil {
		return nil, "", err
	}
	return att, answer, nil
}

func isNotConfiguredError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, platform.ErrNotConfigured) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "не настроена")
}

// cropImageRegion вырезает заданную область изображения схемы в PNG для детального чтения клемм и пинов.
// Поддерживает квадранты с 10% перекрытием (top_left, top_right, bottom_left, bottom_right, center)
// и произвольные нормализованные координаты "x1,y1,x2,y2" в диапазоне 0..1.
func cropImageRegion(data []byte, region string) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("empty image bounds")
	}

	var fx1, fy1, fx2, fy2 float64
	switch region {
	case "top_left":
		fx1, fy1, fx2, fy2 = 0.0, 0.0, 0.55, 0.55
	case "top_right":
		fx1, fy1, fx2, fy2 = 0.45, 0.0, 1.0, 0.55
	case "bottom_left":
		fx1, fy1, fx2, fy2 = 0.0, 0.45, 0.55, 1.0
	case "bottom_right":
		fx1, fy1, fx2, fy2 = 0.45, 0.45, 1.0, 1.0
	case "center":
		fx1, fy1, fx2, fy2 = 0.2, 0.2, 0.8, 0.8
	default:
		parts := strings.Split(region, ",")
		if len(parts) != 4 {
			return nil, fmt.Errorf("unsupported region: %s", region)
		}
		var vals [4]float64
		for i, p := range parts {
			v, parseErr := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if parseErr != nil {
				return nil, parseErr
			}
			vals[i] = clamp01(v)
		}
		fx1, fy1, fx2, fy2 = vals[0], vals[1], vals[2], vals[3]
	}

	x1 := b.Min.X + int(float64(w)*fx1)
	y1 := b.Min.Y + int(float64(h)*fy1)
	x2 := b.Min.X + int(float64(w)*fx2)
	y2 := b.Min.Y + int(float64(h)*fy2)
	if x2 <= x1 || y2 <= y1 {
		return nil, fmt.Errorf("invalid crop rectangle")
	}

	rect := image.Rect(x1, y1, x2, y2).Intersect(b)
	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			dst.Set(x-rect.Min.X, y-rect.Min.Y, img.At(x, y))
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
