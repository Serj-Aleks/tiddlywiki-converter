package tiddlywiki

import (
	"time"
)

// Tiddler представляет собой один "тиддлер" в TiddlyWiki.
type Tiddler struct {
	Title    string `json:"title"`
	Text     string `json:"text"`
	Tags     string `json:"tags,omitempty"` // omitempty - если тегов нет, поле не будет добавлено в JSON
	Created  string `json:"created"`
	Modified string `json:"modified"`
	
	// Карта для хранения любых дополнительных полей.
	// Ключи этой карты станут именами полей в TiddlyWiki.
	Fields map[string]string `json:"-"` // json:"-" означает, что это поле не будет автоматически сериализовано в JSON
}

// tiddlyTimeFormat - это формат времени, который использует TiddlyWiki.
const TiddlyTimeFormat = "20060102150405000"

// NewTiddler теперь возвращает указатель, чтобы было удобнее работать с картой полей
func NewTiddler(title, text, tags string) *Tiddler {
	now := time.Now().UTC().Format(TiddlyTimeFormat)
	return &Tiddler{
		Title:    title,
		Text:     text,
		Tags:     tags,
		Created:  now,
		Modified: now,
		Fields:   make(map[string]string), // Инициализируем карту
	}
}

// ToJSONMap преобразует Tiddler в карту для корректной сериализации в JSON,
// включая пользовательские поля.
func (t *Tiddler) ToJSONMap() map[string]interface{} {
	data := map[string]interface{}{
		"title":    t.Title,
		"text":     t.Text,
		"created":  t.Created,
		"modified": t.Modified,
	}
	if t.Tags != "" {
		data["tags"] = t.Tags
	}
	// Добавляем все наши кастомные поля
	for key, value := range t.Fields {
		data[key] = value
	}
	return data
}