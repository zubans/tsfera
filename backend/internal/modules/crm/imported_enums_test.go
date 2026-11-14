package crm

import "testing"

func TestImportedClassificationsKeepDistinctMeaning(t *testing.T) {
	if !knownEnumID(CallerTypeOptions, 1002) || !knownEnumID(CallResultOptions, 1014) || !isValidCallAppeal("import_14") {
		t.Fatal("imported classification cannot be edited")
	}
	if knownEnumID(CallResultOptions, 1013) || isValidCallAppeal("import_99") {
		t.Fatal("unknown classification accepted")
	}
	report := BuildCallConversionMatrix(map[string]map[int]int{"import_14": {1014: 3}, "import_9": {1001: 2}})
	if report.SummaryRow.Total != 5 || report.TargetRow.Total != 2 {
		t.Fatal("imported calls omitted or classified incorrectly")
	}
	found := false
	for _, row := range report.Rows {
		if row.TypeOfAppeal == "import_14" {
			found = row.Total == 3 && row.Results[1014] == 3
		}
	}
	if !found {
		t.Fatal("feedback collapsed into another appeal")
	}
}

func TestImportedDocumentNumberIsNotDatabaseID(t *testing.T) {
	number := int64(8765)
	if documentNumber(17, &number) != 8765 || documentNumber(17, nil) != 17 {
		t.Fatal("document number replaced by database ID")
	}
}
