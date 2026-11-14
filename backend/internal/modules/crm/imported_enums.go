package crm

import "strconv"

// Import codes have their own namespace: equal numeric IDs in two systems do
// not imply equal meaning. Source labels remain available when editing calls.
func init() {
	for id, label := range []string{"Действующий", "Новый", "Сотрудник", "Спам"} {
		CallerTypeOptions = append(CallerTypeOptions, EnumOption{ID: 1001 + id, Key: "import_" + strconv.Itoa(id+1), Label: label + " (перенесено)"})
	}
	results := map[int]string{1: "Заключение договора", 2: "Продление договора", 3: "Оформление заявки на услугу", 4: "Продажа магазин", 5: "Уточнение", 6: "Отмена заявки", 7: "Отказ по звонку", 8: "Отзыв", 9: "Вопрос в ГК", 10: "Клиент не отвечает", 11: "Думает", 14: "Обратная связь"}
	for id := 1; id <= 14; id++ {
		if label, ok := results[id]; ok {
			CallResultOptions = append(CallResultOptions, EnumOption{ID: 1000 + id, Key: "import_" + strconv.Itoa(id), Label: label + " (перенесено)"})
		}
	}
	appeals := []string{"Заявка на обслуживание, новый клиент", "Заявка на обслуживание, действующий клиент", "Купить запчасти", "Отказ или отмена заявки", "Спам", "Уточнения по заявке или договору", "Вопрос в ГК", "Отзыв клиента", "Договор", "Услуга", "Акция", "Магазин", "Уточнение", "Обратная связь", "Обзвон по действующему договору"}
	for id, label := range appeals {
		ExtendedCallTypesOfAppeal = append(ExtendedCallTypesOfAppeal, EnumOptionLegacy{ID: 1001 + id, Key: "import_" + strconv.Itoa(id+1), Label: label + " (перенесено)", Legacy: true})
	}
}

func knownEnumID(options []EnumOption, id int) bool {
	if id == 0 {
		return true
	}
	for _, option := range options {
		if option.ID == id {
			return true
		}
	}
	return false
}
