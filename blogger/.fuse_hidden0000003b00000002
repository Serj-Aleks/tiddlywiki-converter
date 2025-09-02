package blogger

import (
	"context"
	"fmt"
	"html"
	"log"
	"regexp"
	"strings"
	"time"

	"google.golang.org/api/blogger/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"tiddlywiki-converter/tiddlywiki"
)

// --- УЛУЧШЕННАЯ ФУНКЦИЯ ОЧИСТКИ HTML ---
// Этот регэксп находит основные блочные теги, которые нужно заменить пробелом.
var spaceReplacer = regexp.MustCompile(`(?i)</p>|<br\s*/?>|</div>|</li>`)
// Этот регэксп удаляет все остальные теги.
var tagStripper = regexp.MustCompile("<[^>]*>")
// Этот регэксп убирает лишние пробелы.
var multipleSpaces = regexp.MustCompile(`\s+`)

func stripHTML(input string) string {
	// 1. Заменяем блочные теги на пробелы, чтобы сохранить разделение.
	spaced := spaceReplacer.ReplaceAllString(input, " ")
	// 2. Удаляем все оставшиеся теги.
	stripped := tagStripper.ReplaceAllString(spaced, "")
	// 3. Заменяем множественные пробелы на один.
	cleaned := multipleSpaces.ReplaceAllString(stripped, " ")
	// 4. Убираем пробелы в начале и конце строки.
	return strings.TrimSpace(cleaned)
}

// getBlogIDByURL находит ID блога по его URL.
func getBlogIDByURL(service *blogger.Service, blogURL string) (string, error) {
	log.Printf("Определяем ID блога по URL: %s", blogURL)
	blog, err := service.Blogs.GetByUrl(blogURL).Do()
	if err != nil {
		return "", fmt.Errorf("не удалось получить информацию о блоге по URL '%s': %w", blogURL, err)
	}
	log.Printf("ID блога успешно найден: %s", blog.Id)
	return blog.Id, nil
}

// convertBlogContent выполняет основную работу по конвертации постов и комментариев.
// Она принимает уже созданный сервис и ID блога.
func convertBlogContent(service *blogger.Service, blogID string) ([]*tiddlywiki.Tiddler, error) {
	var allTiddlers []*tiddlywiki.Tiddler

	log.Println("Шаг 1: Загрузка всех постов...")
	posts, err := fetchAllPosts(service, blogID)
	if err != nil {
		return nil, err
	}
	log.Printf("Загружено %d постов.", len(posts))

	log.Println("Шаг 2: Обработка постов и загрузка комментариев...")
	for i, post := range posts {
		// --- ОБРАБОТКА ПОСТА ---
		cleanTitle := html.UnescapeString(post.Title)
		cleanContent := post.Content // Контент поста берем "КАК ЕСТЬ"
		authorName := html.UnescapeString(post.Author.DisplayName)
		cleanContent += fmt.Sprintf("\n\n<p>''Автор: %s''</p>", authorName)

		created, _ := time.Parse(time.RFC3339, post.Published)
		tiddlyTime := created.UTC().Format(tiddlywiki.TiddlyTimeFormat)

		postTiddler := tiddlywiki.NewTiddler(cleanTitle, cleanContent, strings.Join(post.Labels, " "))
		postTiddler.Created = tiddlyTime
		postTiddler.Modified = tiddlyTime
		postTiddler.Fields["post-slug"] = post.Id
		allTiddlers = append(allTiddlers, postTiddler)
		
		// --- ОБРАБОТКА КОММЕНТАРИЕВ ---
		log.Printf("Запрос комментариев для поста %d/%d: %s", i+1, len(posts), cleanTitle)
		comments, err := fetchAllComments(service, blogID, post.Id)
		if err != nil {
			log.Printf(" -> Не удалось получить комментарии: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}
		
		if len(comments) > 0 {
			log.Printf(" -> Найдено %d комментариев.", len(comments))
		}

		commentCounter := 0
		for _, comment := range comments {
			commentCounter++ 

			decodedText := html.UnescapeString(comment.Content)
			cleanCommentText := stripHTML(decodedText)

			commentAuthor := html.UnescapeString(comment.Author.DisplayName)
			
			commentTitle := fmt.Sprintf("Комментарий %d от %s к посту «%s»", commentCounter, commentAuthor, cleanTitle)

			commentCreated, _ := time.Parse(time.RFC3339, comment.Published)
			commentTiddlyTime := commentCreated.UTC().Format(tiddlywiki.TiddlyTimeFormat)

			commentTiddler := tiddlywiki.NewTiddler(
				commentTitle,
				cleanCommentText,
				"comment",
			)
			commentTiddler.Created = commentTiddlyTime
			commentTiddler.Modified = commentTiddlyTime
			commentTiddler.Fields["parent-post"] = post.Id
			allTiddlers = append(allTiddlers, commentTiddler)
		}
		
		time.Sleep(1 * time.Second)
	}

	return allTiddlers, nil
}

// ConvertFromBlogID создает сервис и запускает конвертацию по ID блога.
func ConvertFromBlogID(apiKey, blogID string) ([]*tiddlywiki.Tiddler, error) {
	ctx := context.Background()
	bloggerService, err := blogger.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("не удалось создать сервис Blogger: %w", err)
	}
	return convertBlogContent(bloggerService, blogID)
}

// ConvertFromURL создает сервис, находит ID по URL и запускает конвертацию.
func ConvertFromURL(apiKey, blogURL string) ([]*tiddlywiki.Tiddler, error) {
	ctx := context.Background()
	bloggerService, err := blogger.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("не удалось создать сервис Blogger: %w", err)
	}

	blogID, err := getBlogIDByURL(bloggerService, blogURL)
	if err != nil {
		return nil, err
	}

	return convertBlogContent(bloggerService, blogID)
}

// Функции fetchAllPosts и fetchAllComments остаются без изменений
func fetchAllPosts(service *blogger.Service, blogID string) ([]*blogger.Post, error) {
	var allPosts []*blogger.Post
	var pageToken string
	for {
		call := service.Posts.List(blogID).MaxResults(50)
		if pageToken != "" { 
			call.PageToken(pageToken) 
		}
		postList, err := call.Do()
		if err != nil {
			if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 429 {
				log.Println("Превышен лимит при запросе постов. Ждем 5 секунд...")
				time.Sleep(5 * time.Second)
				continue
			}
			return nil, err
		}
		if len(postList.Items) > 0 {
			log.Printf("...загружено %d постов...", len(postList.Items))
			allPosts = append(allPosts, postList.Items...)
		}
		if postList.NextPageToken == "" { 
			break 
		}
		pageToken = postList.NextPageToken
		time.Sleep(500 * time.Millisecond)
	}
	return allPosts, nil
}

func fetchAllComments(service *blogger.Service, blogID, postID string) ([]*blogger.Comment, error) {
	var allComments []*blogger.Comment
	var pageToken string
	for {
		call := service.Comments.List(blogID, postID).MaxResults(50)
		if pageToken != "" { 
			call.PageToken(pageToken) 
		}
		commentList, err := call.Do()
		if err != nil { 
			if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 404 {
				return nil, nil
			}
			return nil, err 
		}
		if len(commentList.Items) > 0 {
			allComments = append(allComments, commentList.Items...)
		}
		if commentList.NextPageToken == "" { 
			break 
		}
		pageToken = commentList.NextPageToken
		time.Sleep(500 * time.Millisecond)
	}
	return allComments, nil
}