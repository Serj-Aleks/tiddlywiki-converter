package tiddlywiki

import (
	"encoding/json"
	"os"
	"strings"
)

func GenerateHTML(tiddlers []*Tiddler, templatePath, outputPath string) error {
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}
	templateContent := string(templateBytes)

	var tiddlerMaps []map[string]interface{}
	for _, t := range tiddlers {
		tiddlerMaps = append(tiddlerMaps, t.ToJSONMap())
	}

	tiddlersJSON, err := json.MarshalIndent(tiddlerMaps, "", "  ")
	if err != nil {
		return err
	}
	
	// Экранируем закрывающий тег script, чтобы избежать преждевременного
	// закрытия блока <script> в HTML-файле.
	finalJSON := strings.Replace(string(tiddlersJSON), "</script>", "<\\/script>", -1)
	
	// --- СИНТАКСИЧЕСКОЕ ИСПРАВЛЕНИЕ ---
	// Убраны некорректные символы '\' вокруг обратных кавычек.
	oldStore := `<script id="storeArea" type="application/json">
[]
</script>`
	// --- КОНЕЦ ИСПРАВЛЕНИЯ ---

	// Используем исправленную строку finalJSON вместо tiddlersJSON
	newStore := `<script id="storeArea" type="application/json">` + "\n" + finalJSON + "\n</script>"
	finalHTML := strings.Replace(templateContent, oldStore, newStore, 1)

	// Улучшенная проверка на случай, если замена не удалась.
	if finalHTML == templateContent {
		errorMsg := "ОШИБКА: Не удалось найти блок storeArea в шаблоне. Убедитесь, что в файле internal/template.html есть блок:\n\n" + oldStore
		return os.WriteFile(outputPath, []byte(errorMsg), 0644)
	}

	return os.WriteFile(outputPath, []byte(finalHTML), 0644)
}