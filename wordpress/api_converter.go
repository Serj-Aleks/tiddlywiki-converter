package wordpress

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"tiddlywiki-converter/tiddlywiki"
)

// --- Структуры для обоих API ---
type WpComSite struct { Name string `json:"name"`; Description string `json:"description"` }
type WpComPostAuthor struct { Name string `json:"name"` }
type WpComPostTags map[string]struct { Name string `json:"name"` }
type WpComPost struct { ID int `json:"ID"`; URL string `json:"URL"`; Date string `json:"date"`; Title string `json:"title"`; Content string `json:"content"`; Author WpComPostAuthor `json:"author"`; Tags WpComPostTags `json:"tags"`; Slug string `json:"slug"` }
type WpComCommentAuthor struct { Name string `json:"name"` }
type WpComComment struct { ID int `json:"ID"`; URL string `json:"URL"`; Author WpComCommentAuthor `json:"author"`; Date string `json:"date"`; Content string `json:"content"`; Parent interface{} `json:"parent"` }

type SelfHostedSite struct { Name string `json:"name"`; Description string `json:"description"` }
type SelfHostedRenderedField struct { Rendered string `json:"rendered"` }
type SelfHostedEmbeddedData struct { Author []struct { Name string `json:"name"` } `json:"author"`; WpTerm [][]struct { Name string `json:"name"` } `json:"wp:term"` }
type SelfHostedPost struct { ID int `json:"id"`; Date string `json:"date"`; Title SelfHostedRenderedField `json:"title"`; Content SelfHostedRenderedField `json:"content"`; Embedded SelfHostedEmbeddedData `json:"_embedded"`; Slug string `json:"slug"`; Link string `json:"link"` }
type SelfHostedComment struct { ID int `json:"id"`; Post int `json:"post"`; Parent int `json:"parent"`; AuthorName string `json:"author_name"`; Date string `json:"date"`; Content SelfHostedRenderedField `json:"content"`; Link string `json:"link"` }

var tagStripper = regexp.MustCompile("<[^>]*>")
func stripHTML(input string) string { return tagStripper.ReplaceAllString(input, "") }

func ConvertFromURL(siteURL string) ([]*tiddlywiki.Tiddler, error) {
	parsedURL, err := url.Parse(siteURL)
	if err != nil { return nil, fmt.Errorf("некорректный URL: %w", err) }
	host := parsedURL.Host
	isWpCom := strings.HasSuffix(host, ".wordpress.com")

	var allTiddlers []*tiddlywiki.Tiddler

	if isWpCom {
		siteInfo, err := fetchWpComSiteInfo(host)
		if err != nil { log.Printf("Предупреждение: не удалось получить информацию о сайте: %v", err) }
		if siteInfo != nil {
			allTiddlers = append(allTiddlers, tiddlywiki.NewTiddler("$:/SiteTitle", siteInfo.Name, ""))
			allTiddlers = append(allTiddlers, tiddlywiki.NewTiddler("$:/SiteSubtitle", siteInfo.Description, ""))
		}
		
		posts, err := fetchAllWpComPosts(host)
		if err != nil { return nil, err }
		
		for _, post := range posts {
			baseTiddlerTitle := strings.ReplaceAll(html.UnescapeString(post.Title), " ", "_")
			comments, err := fetchAllWpComCommentsForPost(host, post.ID)
			if err != nil { log.Printf("Предупреждение: не удалось загрузить комментарии для поста %d: %v", post.ID, err) }

			var postTags []string
			for _, tag := range post.Tags { postTags = append(postTags, fmt.Sprintf("[[%s]]", tag.Name)) }
			tagsString := strings.Join(postTags, " ")
			created, _ := time.Parse(time.RFC3339, post.Date)
			tiddlyTime := created.UTC().Format(tiddlywiki.TiddlyTimeFormat)
			var postBody strings.Builder
			postBody.WriteString(post.Content)
			postBody.WriteString(fmt.Sprintf("\n\n<p>''Автор: %s''</p>", post.Author.Name))
			postBody.WriteString(fmt.Sprintf("\n\n---\n\n''Оригинал поста:'' <a href=\"%s\" target=\"_blank\">%s</a>", post.URL, post.URL))
			if len(comments) > 0 { postBody.WriteString(fmt.Sprintf("\n\n---\n\n<<list-links \"[tag[%s]]\">>", baseTiddlerTitle)) }
			postTiddler := tiddlywiki.NewTiddler(baseTiddlerTitle, postBody.String(), tagsString)
			postTiddler.Created = tiddlyTime; postTiddler.Modified = tiddlyTime
			allTiddlers = append(allTiddlers, postTiddler)

			commentHierarchy := make(map[int]int); isParentMap := make(map[int]bool)
			for _, comment := range comments { if parentMap, ok := comment.Parent.(map[string]interface{}); ok { if parentID, ok := parentMap["id"].(float64); ok { parentIDInt := int(parentID); if parentIDInt != 0 { commentHierarchy[comment.ID] = parentIDInt; isParentMap[parentIDInt] = true; } } } }
			for _, comment := range comments {
				parentTiddlerTitle := baseTiddlerTitle
				if parentID, ok := commentHierarchy[comment.ID]; ok { parentTiddlerTitle = fmt.Sprintf("%s-comment-%d", baseTiddlerTitle, parentID) }
				commentTiddlerTitle := fmt.Sprintf("%s-comment-%d", baseTiddlerTitle, comment.ID)
				var commentBody strings.Builder
				commentBody.WriteString(fmt.Sprintf("''Автор:'' %s\n", comment.Author.Name)); commentBody.WriteString(fmt.Sprintf("''Оригинал:'' <a href=\"%s\" target=\"_blank\">ссылка</a>\n\n---\n\n", comment.URL)); commentBody.WriteString(comment.Content)
				if isParentMap[comment.ID] { commentBody.WriteString(fmt.Sprintf("\n\n---\n\n<<list-links \"[tag[%s]]\">>", commentTiddlerTitle)) }
				createdComm, _ := time.Parse(time.RFC3339, comment.Date)
				tiddlyCommTime := createdComm.UTC().Format(tiddlywiki.TiddlyTimeFormat)
				commentTiddler := tiddlywiki.NewTiddler(commentTiddlerTitle, commentBody.String(), fmt.Sprintf("[[%s]]", parentTiddlerTitle))
				commentTiddler.Created = tiddlyCommTime; commentTiddler.Modified = tiddlyCommTime
				allTiddlers = append(allTiddlers, commentTiddler)
			}
		}
	} else {
		// --- ЛОГИКА ДЛЯ САМОХОСТИНГА ---
		siteInfo, err := fetchSelfHostedSiteInfo(host)
		if err != nil { log.Printf("Предупреждение: не удалось получить информацию о сайте: %v", err) }
		if siteInfo != nil {
			allTiddlers = append(allTiddlers, tiddlywiki.NewTiddler("$:/SiteTitle", siteInfo.Name, ""))
			allTiddlers = append(allTiddlers, tiddlywiki.NewTiddler("$:/SiteSubtitle", siteInfo.Description, ""))
		}

		posts, err := fetchAllSelfHostedPosts(host)
		if err != nil { return nil, err }
		postMap := make(map[int]SelfHostedPost);
		for _, post := range posts { postMap[post.ID] = post }
		
		comments, err := fetchAllSelfHostedComments(host)
		if err != nil { log.Printf("Предупреждение: не удалось загрузить комментарии: %v", err) }

		commentHierarchy := make(map[int]int); isParentMap := make(map[int]bool)
		for _, comment := range comments { if comment.Parent != 0 { commentHierarchy[comment.ID] = comment.Parent; isParentMap[comment.Parent] = true; } }
		
		for _, post := range posts {
			cleanTitle := html.UnescapeString(post.Title.Rendered)
			baseTiddlerTitle := strings.ReplaceAll(cleanTitle, " ", "_")
			var postTags []string
			if len(post.Embedded.WpTerm) > 0 { for _, termList := range post.Embedded.WpTerm { for _, term := range termList { postTags = append(postTags, fmt.Sprintf("[[%s]]", term.Name)) } } }
			tagsString := strings.Join(postTags, " ")
			created, _ := time.Parse(time.RFC3339, post.Date)
			tiddlyTime := created.UTC().Format(tiddlywiki.TiddlyTimeFormat)
			var postBody strings.Builder
			postBody.WriteString(html.UnescapeString(post.Content.Rendered))
			if len(post.Embedded.Author) > 0 { authorName := html.UnescapeString(post.Embedded.Author[0].Name); postBody.WriteString(fmt.Sprintf("\n\n<p>''Автор: %s''</p>", authorName)) }
			postBody.WriteString(fmt.Sprintf("\n\n---\n\n''Оригинал поста:'' <a href=\"%s\" target=\"_blank\">%s</a>", post.Link, post.Link))
			postBody.WriteString(fmt.Sprintf("\n\n---\n\n<<list-links \"[tag[%s]]\">>", baseTiddlerTitle))
			postTiddler := tiddlywiki.NewTiddler(baseTiddlerTitle, postBody.String(), tagsString)
			postTiddler.Created = tiddlyTime; postTiddler.Modified = tiddlyTime
			allTiddlers = append(allTiddlers, postTiddler)
		}

		for _, comment := range comments {
			post, ok := postMap[comment.Post]
			if !ok { continue }
			parentPostTitle := strings.ReplaceAll(html.UnescapeString(post.Title.Rendered), " ", "_")
			parentTiddlerTitle := parentPostTitle
			if parentID, ok := commentHierarchy[comment.ID]; ok { parentTiddlerTitle = fmt.Sprintf("%s-comment-%d", parentPostTitle, parentID) }
			commentTiddlerTitle := fmt.Sprintf("%s-comment-%d", parentPostTitle, comment.ID)
			var commentBody strings.Builder
			commentBody.WriteString(fmt.Sprintf("''Автор:'' %s\n", comment.AuthorName)); commentBody.WriteString(fmt.Sprintf("''Оригинал:'' <a href=\"%s\" target=\"_blank\">ссылка</a>\n\n---\n\n", comment.Link)); commentBody.WriteString(html.UnescapeString(comment.Content.Rendered))
			if isParentMap[comment.ID] { commentBody.WriteString(fmt.Sprintf("\n\n---\n\n<<list-links \"[tag[%s]]\">>", commentTiddlerTitle)) }
			createdComm, _ := time.Parse(time.RFC3339, comment.Date)
			tiddlyCommTime := createdComm.UTC().Format(tiddlywiki.TiddlyTimeFormat)
			commentTiddler := tiddlywiki.NewTiddler(commentTiddlerTitle, commentBody.String(), fmt.Sprintf("[[%s]]", parentTiddlerTitle))
			commentTiddler.Created = tiddlyCommTime; commentTiddler.Modified = tiddlyCommTime
			allTiddlers = append(allTiddlers, commentTiddler)
		}
	}
	return allTiddlers, nil
}

// --- Функции загрузки ---
func fetchWpComSiteInfo(host string) (*WpComSite, error) { apiURL := fmt.Sprintf("https://public-api.wordpress.com/rest/v1.1/sites/%s", host); resp, err := http.Get(apiURL); if err != nil { return nil, err }; defer resp.Body.Close(); if resp.StatusCode != http.StatusOK { return nil, fmt.Errorf("статус: %s", resp.Status) }; body, err := io.ReadAll(resp.Body); if err != nil { return nil, err }; var siteInfo WpComSite; if err := json.Unmarshal(body, &siteInfo); err != nil { return nil, err }; return &siteInfo, nil }
func fetchAllWpComPosts(host string) ([]WpComPost, error) { var allPosts []WpComPost; page := 1; for { apiURL := fmt.Sprintf("https://public-api.wordpress.com/rest/v1.1/sites/%s/posts?page=%d&fields=ID,URL,date,title,content,author,tags,slug", host, page); log.Printf("Запрос к API постов: %s", apiURL); resp, err := http.Get(apiURL); if err != nil { return nil, err }; defer resp.Body.Close(); if resp.StatusCode != http.StatusOK { if page > 1 { break }; return nil, fmt.Errorf("статус: %s", resp.Status) }; body, err := io.ReadAll(resp.Body); if err != nil { return nil, err }; var apiResponse struct { Posts []WpComPost `json:"posts"` }; if err := json.Unmarshal(body, &apiResponse); err != nil { return nil, err }; if len(apiResponse.Posts) == 0 { break }; allPosts = append(allPosts, apiResponse.Posts...); log.Printf("Загружено %d постов со страницы %d.", len(apiResponse.Posts), page); page++; time.Sleep(250 * time.Millisecond) }; return allPosts, nil }
func fetchAllWpComCommentsForPost(host string, postID int) ([]WpComComment, error) { var allComments []WpComComment; apiURL := fmt.Sprintf("https://public-api.wordpress.com/rest/v1.1/sites/%s/posts/%d/replies/?order=ASC", host, postID); log.Printf("   -> Запрос комментариев: %s", apiURL); resp, err := http.Get(apiURL); if err != nil { return nil, err }; defer resp.Body.Close(); if resp.StatusCode != http.StatusOK { return nil, fmt.Errorf("статус: %s", resp.Status) }; body, err := io.ReadAll(resp.Body); if err != nil { return nil, err }; var apiResponse struct { Comments []WpComComment `json:"comments"` }; if err := json.Unmarshal(body, &apiResponse); err != nil { return nil, err }; allComments = append(allComments, apiResponse.Comments...); log.Printf("   <- Найдено %d комментариев.", len(allComments)); time.Sleep(250 * time.Millisecond); return allComments, nil }
func fetchSelfHostedSiteInfo(host string) (*SelfHostedSite, error) { apiURL := fmt.Sprintf("https://%s/wp-json/", host); resp, err := http.Get(apiURL); if err != nil { return nil, err }; defer resp.Body.Close(); if resp.StatusCode != http.StatusOK { return nil, fmt.Errorf("статус: %s", resp.Status) }; body, err := io.ReadAll(resp.Body); if err != nil { return nil, err }; var siteInfo SelfHostedSite; if err := json.Unmarshal(body, &siteInfo); err != nil { return nil, err }; return &siteInfo, nil }
func fetchAllSelfHostedPosts(host string) ([]SelfHostedPost, error) { var allPosts []SelfHostedPost; page := 1; for { apiURL := fmt.Sprintf("https://%s/wp-json/wp/v2/posts?page=%d&_embed=author,wp:term", host, page); log.Printf("Запрос к API постов: %s", apiURL); resp, err := http.Get(apiURL); if err != nil { return nil, err }; defer resp.Body.Close(); if resp.StatusCode != http.StatusOK { if page > 1 { break }; return nil, fmt.Errorf("статус: %s", resp.Status) }; body, err := io.ReadAll(resp.Body); if err != nil { return nil, err }; var posts []SelfHostedPost; if err := json.Unmarshal(body, &posts); err != nil { return nil, err }; if len(posts) == 0 { break }; allPosts = append(allPosts, posts...); log.Printf("Загружено %d постов со страницы %d.", len(posts), page); page++; time.Sleep(250 * time.Millisecond) }; return allPosts, nil }
func fetchAllSelfHostedComments(host string) ([]SelfHostedComment, error) { var allComments []SelfHostedComment; page := 1; for { apiURL := fmt.Sprintf("https://%s/wp-json/wp/v2/comments?page=%d&per_page=100&order=asc", host, page); log.Printf("Запрос к API комментариев: %s", apiURL); resp, err := http.Get(apiURL); if err != nil { return nil, err }; defer resp.Body.Close(); if resp.StatusCode != http.StatusOK { if page > 1 { break }; return nil, fmt.Errorf("статус: %s", resp.Status) }; body, err := io.ReadAll(resp.Body); if err != nil { return nil, err }; var comments []SelfHostedComment; if err := json.Unmarshal(body, &comments); err != nil { return nil, err }; if len(comments) == 0 { break }; allComments = append(allComments, comments...); log.Printf("Загружено %d комментариев со страницы %d.", len(comments), page); page++; time.Sleep(250 * time.Millisecond) }; return allComments, nil }