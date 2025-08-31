package wordpress

import (
	"bytes" // Импортируем новый пакет
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"tiddlywiki-converter/tiddlywiki"
)

// --- ВОЗВРАЩАЕМСЯ К ПРОСТЫМ СТРУКТУРАМ ---
type WXRFile struct {
	Channel struct {
		Items []Item `xml:"item"`
	} `xml:"channel"`
}

type Item struct {
	Title      string     `xml:"title"`
	PubDate    string     `xml:"pubDate"`
	// ИЗМЕНЕНО: Ищем новый тег без префикса
	Content    string     `xml:"content_encoded"`
	PostID     int        `xml:"post_id"`
	PostType   string     `xml:"post_type"`
	Status     string     `xml:"status"`
	Categories []Category `xml:"category"`
	Comments   []Comment  `xml:"comment"`
}

type Category struct {
	Domain   string `xml:"domain,attr"`
	Value    string `xml:",cdata"`
}

type Comment struct {
	Author  string `xml:"comment_author"`
	DateGMT string `xml:"comment_date_gmt"`
	Content string `xml:"comment_content"`
}

func ConvertFromXMLFile(filePath string) ([]*tiddlywiki.Tiddler, error) {
	xmlFile, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия файла %s: %w", filePath, err)
	}
	defer xmlFile.Close()

	byteValue, _ := io.ReadAll(xmlFile)

	// --- НОВЫЙ ТРЮК: УДАЛЯЕМ ПРОБЛЕМНЫЕ ПРЕФИКСЫ ---
	// Заменяем "content:encoded" на "content_encoded", "dc:creator" на "creator" и т.д.
	byteValue = bytes.ReplaceAll(byteValue, []byte("<content:encoded>"), []byte("<content_encoded>"))
	byteValue = bytes.ReplaceAll(byteValue, []byte("</content:encoded>"), []byte("</content_encoded>"))
	// Можно добавить и другие замены для wp:, dc: и т.д., если они понадобятся
	// --- КОНЕЦ ТРЮКА ---

	var wxr WXRFile
	if err := xml.Unmarshal(byteValue, &wxr); err != nil {
		return nil, fmt.Errorf("ошибка парсинга XML: %w", err)
	}

	var tiddlers []*tiddlywiki.Tiddler

	for _, item := range wxr.Channel.Items {
		if item.PostType != "post" || item.Status != "publish" {
			continue
		}
		
		postTitle := item.Title
		postID := fmt.Sprintf("%d", item.PostID)

		var postTags []string
		for _, cat := range item.Categories {
			if cat.Domain == "post_tag" {
				postTags = append(postTags, cat.Value)
			}
		}
		tagsString := strings.Join(postTags, " ")

		created, _ := time.Parse(time.RFC1123Z, item.PubDate)
		tiddlyTime := created.UTC().Format(tiddlywiki.TiddlyTimeFormat)

		postTiddler := tiddlywiki.NewTiddler(postTitle, item.Content, tagsString)
		postTiddler.Created = tiddlyTime
		postTiddler.Modified = tiddlyTime
		postTiddler.Fields["post-id"] = postID
		tiddlers = append(tiddlers, postTiddler)

		for _, comment := range item.Comments {
			author := comment.Author
			commentText := fmt.Sprintf("<blockquote>%s</blockquote>\n\n''- %s''", comment.Content, author)
			createdComm, _ := time.Parse("2006-01-02 15:04:05", comment.DateGMT)
			tiddlyCommTime := createdComm.UTC().Format(tiddlywiki.TiddlyTimeFormat)

			commentTiddler := tiddlywiki.NewTiddler(
				fmt.Sprintf("Комментарий от %s к посту «%s»", author, postTitle),
				commentText, "comment",
			)
			commentTiddler.Created = tiddlyCommTime
			commentTiddler.Modified = tiddlyCommTime
			commentTiddler.Fields["parent-post"] = postID
			tiddlers = append(tiddlers, commentTiddler)
		}
	}

	return tiddlers, nil
}