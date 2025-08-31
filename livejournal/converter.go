package livejournal

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"tiddlywiki-converter/tiddlywiki"
	"golang.org/x/net/html"
)

type LivejournalPost struct {
	Title       string
	URL         string
	Description string
	Body        string
	Tags        []string
}

// =============================================================================
// ЭКСПОРТИРУЕМЫЙ ИНТЕРФЕЙС
// =============================================================================

func ConvertBlogFromURL(blogURL string) ([]*tiddlywiki.Tiddler, error) {
	return ConvertFromURL(blogURL)
}

func ConvertBlogForYear(blogURL string, year int) ([]*tiddlywiki.Tiddler, error) {
	u, err := url.Parse(blogURL)
	if err != nil { return nil, fmt.Errorf("некорректный URL блога: %w", err) }
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	yearURL := fmt.Sprintf("%s/%d/", u.String(), year)
	return ConvertFromURL(yearURL)
}

func ConvertBlogForMonth(blogURL string, year int, month time.Month) ([]*tiddlywiki.Tiddler, error) {
	u, err := url.Parse(blogURL)
	if err != nil { return nil, fmt.Errorf("некорректный URL блога: %w", err) }
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	monthURL := fmt.Sprintf("%s/%d/%02d/", u.String(), year, int(month))
	return ConvertFromURL(monthURL)
}

// =============================================================================
// ГЛАВНЫЙ ДИСПЕТЧЕР И ОРКЕСТРАТОР
// =============================================================================

// ConvertFromURL - главный диспетчер. Анализирует URL и запускает нужную логику.
func ConvertFromURL(pageURL string) ([]*tiddlywiki.Tiddler, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	
	// Определяем тип URL с помощью регулярных выражений
	isPost, _ := regexp.MatchString(`/\d+\.html$`, pageURL)
	isYear, _ := regexp.MatchString(`/\d{4}/?$`, pageURL)
	isMonth, _ := regexp.MatchString(`/\d{4}/\d{2}/?$`, pageURL)
	isDay, _ := regexp.MatchString(`/\d{4}/\d{2}/\d{2}/?$`, pageURL)

	// Создаем системные тиддлеры один раз в самом начале.
	allTiddlers, _, err := createSystemTiddlers(pageURL, client)
	if err != nil {
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: не удалось создать системные тиддлеры: %v", err)
		allTiddlers = make([]*tiddlywiki.Tiddler, 0)
	}

	if isPost {
		log.Printf("Обнаружен URL поста. Конвертируется один пост: %s", pageURL)
		postTiddlers, err := convertSinglePost(pageURL, client)
		if err != nil { return nil, err }
		allTiddlers = append(allTiddlers, postTiddlers...)

	} else if isDay || isMonth {
		log.Printf("Обнаружен URL архива за месяц/день. Сканируется одна страница: %s", pageURL)
		archiveTiddlers, err := processArchivePage(pageURL, client)
		if err != nil { return nil, err }
		allTiddlers = append(allTiddlers, archiveTiddlers...)

	} else if isYear {
		log.Printf("Обнаружен URL архива за год. Запускается цикл по месяцам для: %s", pageURL)
		u, _ := url.Parse(pageURL)
		baseURL := strings.TrimSuffix(u.String(), "/")
		
		for month := 1; month <= 12; month++ {
			monthlyURL := fmt.Sprintf("%s/%02d/", baseURL, month)
			log.Printf("-> Обрабатывается месяц: %s", monthlyURL)
			archiveTiddlers, err := processArchivePage(monthlyURL, client)
			if err != nil { 
				log.Printf("   ! Ошибка обработки месяца %s (возможно, его не существует): %v", monthlyURL, err)
				continue
			}
			allTiddlers = append(allTiddlers, archiveTiddlers...)
		}
	} else {
		// Если это не пост, не год, не месяц и не день - считаем, что это весь блог.
		// Используем ВАШУ НАДЕЖНУЮ ЛОГИКУ ПОЛНОГО ОБХОДА.
		log.Printf("Обнаружен URL блога. Запускается полный обход архивов: %s", pageURL)
		u, _ := url.Parse(pageURL)
		baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

		startYear, _ := getBlogStartYear(baseURL, client) // Ваша функция-заглушка
		currentYear := time.Now().Year()

		for year := startYear; year <= currentYear; year++ {
			log.Printf("==> Обрабатывается год: %d", year)
			for month := 1; month <= 12; month++ {
				monthlyURL := fmt.Sprintf("%s/%d/%02d/", baseURL, year, month)
				log.Printf("-> Обрабатывается месяц: %s", monthlyURL)
				archiveTiddlers, err := processArchivePage(monthlyURL, client)
				if err != nil {
					log.Printf("   ! Ошибка обработки месяца %s (возможно, его не существует): %v", monthlyURL, err)
					continue
				}
				allTiddlers = append(allTiddlers, archiveTiddlers...)
			}
		}
	}

	log.Printf("Конвертация завершена. Всего создано тиддлеров: %d", len(allTiddlers))
	return allTiddlers, nil
}

// processArchivePage - рабочая лошадка для месячных/дневных архивов.
// Сканирует ОДНУ страницу, находит посты и запускает их параллельную обработку.
func processArchivePage(pageURL string, client *http.Client) ([]*tiddlywiki.Tiddler, error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil { return nil, err }
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("страница архива не найдена (404)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("статус %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil { return nil, err }

	baseURL, _ := url.Parse(pageURL)
	postURLs := make(chan string, 100)
	go func() {
		defer close(postURLs)
		streamPostsFromArchiveGreedy(bytes.NewReader(bodyBytes), baseURL, func(postURL string) {
			postURLs <- postURL
		})
	}()

	var wg sync.WaitGroup
	tiddlerChan := make(chan []*tiddlywiki.Tiddler, 10)
	workerLimit := 10
	guard := make(chan struct{}, workerLimit)

	// =========================================================================
	// ФИНАЛЬНОЕ ИСПРАВЛЕНИЕ: ПРАВИЛЬНАЯ ОРКЕСТРАЦИЯ
	// =========================================================================

	// ШАГ 1: Запускаем горутину, которая будет ДИСПЕТЧЕРОМ.
	// Ее задача - читать URL-ы и запускать для них воркеров.
	go func() {
		for postURL := range postURLs {
			wg.Add(1)
			guard <- struct{}{}
			go func(pURL string) {
				defer wg.Done()
				defer func() { <-guard }()
				tiddlers, err := convertSinglePost(pURL, client)
				if err != nil {
					log.Printf("! Ошибка конвертации поста %s: %v", pURL, err)
					return
				}
				tiddlerChan <- tiddlers
			}(postURL)
		}

		// ШАГ 2: После того как все воркеры ЗАПУЩЕНЫ, диспетчер ждет их завершения.
		wg.Wait()
		
		// ШАГ 3: После того как все воркеры ЗАВЕРШЕНЫ, диспетчер закрывает канал результатов.
		// Это сигнал для главного потока, что работа окончена.
		close(tiddlerChan)
	}()

	// ШАГ 4: Главный поток НЕ ЖДЕТ. Он НЕМЕДЛЕННО начинает принимать результаты.
	// Этот цикл работает параллельно с диспетчером и воркерами.
	var allTiddlers []*tiddlywiki.Tiddler
	for tiddlers := range tiddlerChan {
		allTiddlers = append(allTiddlers, tiddlers...)
	}
	// =========================================================================

	return allTiddlers, nil
}

// =============================================================================
// ВСЕ ВАШИ РАБОЧИЕ ФУНКЦИИ - БЕЗ ИЗМЕНЕНИЙ
// =============================================================================

// convertSinglePost загружает, парсит один пост и ИЗВЛЕКАЕТ ДЛЯ НЕГО ВСЕ КОММЕНТАРИИ.
func convertSinglePost(pageURL string, client *http.Client) ([]*tiddlywiki.Tiddler, error) {
	log.Printf("    -> Начата обработка поста: %s", pageURL)

	// =========================================================================
	// НОВАЯ ЛОГИКА С ДВУМЯ ЗАПРОСАМИ
	// =========================================================================

	// --- ШАГ 1: Загружаем страницу поста, чтобы извлечь ТЕКСТ ПОСТА ---
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil { return nil, err }
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	
	resp, err := client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return nil, fmt.Errorf("статус %d при запросе поста", resp.StatusCode) }
	
	postBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil { return nil, err }

	// Парсим информацию о самом посте (заголовок, тело, теги)
	post, err := parsePostPage(postBodyBytes)
	if err != nil { return nil, err }

	// --- ШАГ 2: Загружаем страницу комментариев, чтобы извлечь ВСЕ КОММЕНТАРИИ ---
	commentsURL := pageURL + "?view=comments"
	log.Printf("       -> Загрузка комментариев со страницы: %s", commentsURL)
	
	commentsReq, err := http.NewRequest("GET", commentsURL, nil)
	if err != nil { return nil, err }
	commentsReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	commentsResp, err := client.Do(commentsReq)
	if err != nil { return nil, err }
	defer commentsResp.Body.Close()
	if commentsResp.StatusCode != http.StatusOK { return nil, fmt.Errorf("статус %d при запросе комментариев", commentsResp.StatusCode) }

	commentsBodyBytes, err := io.ReadAll(commentsResp.Body)
	if err != nil { return nil, err }

	// --- ШАГ 3: Собираем все вместе ---

	if post.URL != "" {
		post.Body += fmt.Sprintf("\n\n---\n\n''Оригинал поста:'' <a href=\"%s\" target=\"_blank\">%s</a>", post.URL, post.URL)
	}
	post.Body += fmt.Sprintf("\n\n---\n\n<<list-links \"[tag[%s]]\">>", post.Title)
	
	var tagsBuilder strings.Builder
	for _, tag := range post.Tags {
		if strings.Contains(tag, " ") {
			tagsBuilder.WriteString(fmt.Sprintf("[[%s]] ", tag))
		} else {
			tagsBuilder.WriteString(tag)
			tagsBuilder.WriteString(" ")
		}
	}
	tagsString := strings.TrimSpace(tagsBuilder.String())

	postTiddler := tiddlywiki.NewTiddler(post.Title, post.Body, tagsString)
	postTiddler.Fields["url"] = post.URL
	
	allTiddlers := []*tiddlywiki.Tiddler{postTiddler}

	// Передаем HTML со страницы комментариев в наш парсер
	commentTiddlers := parseCommentsFromRenderedHTML(commentsBodyBytes, post.Title)
	if commentTiddlers != nil {
		allTiddlers = append(allTiddlers, commentTiddlers...)
	}

	log.Printf("    <- Пост '%s' завершен. Всего тиддлеров: %d (1 пост + %d коммент.)", post.Title, len(allTiddlers), len(commentTiddlers))
	return allTiddlers, nil
}

func streamPostsFromArchiveGreedy(body io.Reader, baseURL *url.URL, callback func(postURL string)) error {
	doc, err := html.Parse(body)
	if err != nil { return err }
	processedLinks := make(map[string]bool)
	var bodyNode *html.Node

	var findBody func(*html.Node)
	findBody = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "body" {
			bodyNode = n
			return
		}
		for c := n.FirstChild; c != nil && bodyNode == nil; c = c.NextSibling { findBody(c) }
	}
	findBody(doc)

	if bodyNode == nil { return fmt.Errorf("тег <body> не найден на странице архива") }

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := getAttr(n, "href")
			if href != "" {
				postURL, err := url.Parse(href)
				if err == nil {
					re := regexp.MustCompile(`/\d+\.html`)
					isPostLink := (postURL.Host == "" || postURL.Host == baseURL.Host) && re.MatchString(href)

					if isPostLink {
						if !strings.Contains(href, "?thread=") && !strings.Contains(href, "#comments") {
							absURL := resolveURL(baseURL, href)
							cleanAbsURL := cleanURL(absURL)
							if !processedLinks[cleanAbsURL] {
								processedLinks[cleanAbsURL] = true
								callback(cleanAbsURL)
							}
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling { traverse(c) }
	}
	traverse(bodyNode)
	return nil
}

func createSystemTiddlers(baseURL string, client *http.Client) ([]*tiddlywiki.Tiddler, string, error) {
	req, err := http.NewRequest("GET", baseURL, nil)
	if err != nil { return nil, "", err }
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil { return nil, "", err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return nil, "", fmt.Errorf("статус %d при запросе %s", resp.StatusCode, baseURL)}
	htmlBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil { return nil, "", err }
	doc, err := html.Parse(bytes.NewReader(htmlBodyBytes))
	if err != nil { return nil, "", err }

	var fullTitleText, faviconURL string
	
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if fullTitleText == "" && n.FirstChild != nil { 
					fullTitleText = n.FirstChild.Data 
				}
			// =========================================================================
			// ИЗМЕНЕНИЕ: ОСТАВЛЯЕМ ТОЛЬКО ВАШ НАДЕЖНЫЙ МЕТОД
			// =========================================================================
			case "script":
				if faviconURL == "" && n.FirstChild != nil {
					scriptContent := n.FirstChild.Data
					// Ищем ТОЛЬКО url_userpic. Никаких других вариантов.
					key := `"url_userpic":"`
					if i := strings.Index(scriptContent, key); i != -1 {
						s := scriptContent[i+len(key):]
						if j := strings.Index(s, `"`); j != -1 { 
							faviconURL = strings.ReplaceAll(s[:j], `\/`, `/`) 
						}
					}
				}
			// РЕЗЕРВНАЯ ЛОГИКА С <LINK> ПОЛНОСТЬЮ УДАЛЕНА
			// =========================================================================
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling { f(c) }
	}
	f(doc)
	
	var finalTitle, finalSubtitle string
	platformParts := strings.SplitN(fullTitleText, " — ", 2)
	mainContent := platformParts[0]
	platformName := ""
	if len(platformParts) > 1 {
		platformName = platformParts[1]
	}

	lastColonIndex := strings.LastIndex(mainContent, ": ")
	if lastColonIndex != -1 {
		finalTitle = strings.TrimSpace(mainContent[:lastColonIndex])
		blogName := strings.TrimSpace(mainContent[lastColonIndex+1:])
		if platformName != "" {
			finalSubtitle = fmt.Sprintf("%s: %s", blogName, platformName)
		} else {
			finalSubtitle = blogName
		}
	} else {
		finalTitle = mainContent
		finalSubtitle = platformName
	}
	
	allTiddlers := make([]*tiddlywiki.Tiddler, 0, 4)
	allTiddlers = append(allTiddlers, tiddlywiki.NewTiddler("$:/SiteTitle", finalTitle, ""))
	allTiddlers = append(allTiddlers, tiddlywiki.NewTiddler("$:/SiteSubtitle", finalSubtitle, ""))
	allTiddlers = append(allTiddlers, tiddlywiki.NewTiddler("$:/DefaultTiddlers", "[list[$:/StoryList]]", ""))
	
	if faviconURL != "" {
		favAbsURL, err := url.Parse(faviconURL)
		if err == nil {
			if !favAbsURL.IsAbs() {
				base, _ := url.Parse(baseURL)
				faviconURL = base.ResolveReference(favAbsURL).String()
			}
		}

		favReq, _ := http.NewRequest("GET", faviconURL, nil)
		if favReq != nil {
			favResp, err := client.Do(favReq)
			if err == nil && favResp != nil && favResp.StatusCode == http.StatusOK {
				faviconBytes, _ := io.ReadAll(favResp.Body)
				favResp.Body.Close()
				faviconTiddler := tiddlywiki.NewTiddler("$:/favicon.ico", "", "")
				faviconTiddler.Text = base64.StdEncoding.EncodeToString(faviconBytes)
				faviconTiddler.Fields["type"] = favResp.Header.Get("Content-Type")
				allTiddlers = append(allTiddlers, faviconTiddler)
			}
		}
	}
	
	return allTiddlers, finalTitle, nil
}

func parseCommentsFromRenderedHTML(htmlBody []byte, postTitle string) []*tiddlywiki.Tiddler {
	re := regexp.MustCompile(`Site\.page\s*=\s*({.*?});`)
	allMatches := re.FindAllSubmatch(htmlBody, -1)
	if len(allMatches) == 0 { return nil }

	var largestJSON []byte
	for _, match := range allMatches {
		if len(match) > 1 { if len(match[1]) > len(largestJSON) { largestJSON = match[1] } }
	}
	if largestJSON == nil { return nil }

	var sitePage map[string]interface{}
	if err := json.Unmarshal(largestJSON, &sitePage); err != nil { return nil }

	commentsData, ok := sitePage["comments"].([]interface{})
	if !ok { return nil }
	log.Printf("   -> Массив 'comments' успешно извлечен. Всего объектов: %d.", len(commentsData))

	hierarchyMap := make(map[float64]float64)
	isParentMap := make(map[float64]bool)
	for _, comm := range commentsData {
		commentMap, _ := comm.(map[string]interface{})
		var cID, pID float64
		if id, ok := commentMap["thread"].(float64); ok { cID = id } else if id, ok := commentMap["dtalkid"].(float64); ok { cID = id }
		if cID == 0 { continue }
		
		if parentID, ok := commentMap["parent"].(float64); ok { pID = parentID } else if parentID, ok := commentMap["above"].(float64); ok { pID = parentID }
		if pID != 0 {
			hierarchyMap[cID] = pID
			isParentMap[pID] = true
		}
	}

	var tiddlers []*tiddlywiki.Tiddler
	for _, comm := range commentsData {
		commentMap, _ := comm.(map[string]interface{})
		var commentID float64
		if id, ok := commentMap["thread"].(float64); ok { commentID = id } else if id, ok := commentMap["dtalkid"].(float64); ok { commentID = id }
		if commentID == 0 { continue }
		
		parentID := hierarchyMap[commentID]
		parentTitle := postTitle
		if pID, ok := hierarchyMap[commentID]; ok { parentTitle = fmt.Sprintf("%s-comment-%.0f", postTitle, pID) }
		
		newTiddlerTitle := fmt.Sprintf("%s-comment-%.0f", postTitle, commentID)
		
		var articleText string
		if article, exists := commentMap["article"]; exists && article != nil { articleText, _ = article.(string) } else { articleText = "''Комментарий скрыт или удален.'' //(article: null)//" }
		
		author, _ := commentMap["dname"].(string)
		datetime, _ := commentMap["ctime"].(string)
		commentURL, _ := commentMap["thread_url"].(string)

		var tiddlerTextBuilder strings.Builder
		tiddlerTextBuilder.WriteString(fmt.Sprintf("''Автор:'' %s\n", author))
		tiddlerTextBuilder.WriteString(fmt.Sprintf("''Дата:'' %s\n", datetime))
		if commentURL != "" { tiddlerTextBuilder.WriteString(fmt.Sprintf("''Ссылка:'' <a href=\"%s\" target=\"_blank\">%s</a>\n", commentURL, commentURL)) }
		tiddlerTextBuilder.WriteString("\n---\n\n")
		tiddlerTextBuilder.WriteString(articleText)

		if isParentMap[commentID] { tiddlerTextBuilder.WriteString(fmt.Sprintf("\n\n---\n\n<<list-links \"[tag[%s]]\">>", newTiddlerTitle)) }
		
		var tag string
		if parentID == 0 { tag = fmt.Sprintf("[[%s]]", postTitle) } else { tag = fmt.Sprintf("[[%s]]", parentTitle) }
		
		tiddler := tiddlywiki.NewTiddler(newTiddlerTitle, tiddlerTextBuilder.String(), tag)
		tiddlers = append(tiddlers, tiddler)
	}
	log.Printf("   -> Обработка завершена. Всего создано %d тиддлеров-комментариев.", len(tiddlers))
	return tiddlers
}

func parsePostPage(htmlBody []byte) (*LivejournalPost, error) {
	doc, err := html.Parse(bytes.NewReader(htmlBody))
	if err != nil { return nil, err }
	post := &LivejournalPost{Tags: make([]string, 0)}
	var bodyNode *html.Node
	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			property, content := getAttr(n, "property"), getAttr(n, "content")
			switch property {
			case "og:title": if post.Title == "" { post.Title = content }
			case "og:url": if post.URL == "" { post.URL = content }
			case "og:description": if post.Description == "" { post.Description = content }
			case "article:tag": if content != "" { post.Tags = append(post.Tags, content) }
			}
		}
		if bodyNode == nil && n.Type == html.ElementNode && n.Data == "div" {
			class := getAttr(n, "class")
			if strings.Contains(class, "entry-content") || strings.Contains(class, "aentry-post__text") || strings.Contains(class, "asset-body") {
				bodyNode = n
				return
			}
		}
		if bodyNode == nil { for c := n.FirstChild; c != nil; c = c.NextSibling { traverse(c) } }
	}
	traverse(doc)
	if post.Title == "" {
		if titleNode := findNode(doc, "title"); titleNode != nil { post.Title = getTitleText(titleNode) }
	}
	if post.Title == "" { return nil, fmt.Errorf("не удалось найти заголовок поста") }
	if bodyNode != nil {
		bodyHTML, err := renderInnerNode(bodyNode)
		if err != nil { return nil, fmt.Errorf("не удалось отрендерить тело поста: %w", err) }
		post.Body = bodyHTML
	} else {
		post.Body = "Тело поста не найдено."
	}
	return post, nil
}

func getBlogStartYear(baseURL string, client *http.Client) (int, error) {
	return 1999, nil
}

func getAttr(n *html.Node, key string) string { for _, attr := range n.Attr { if attr.Key == key { return attr.Val } }; return "" }
func findNode(n *html.Node, tagName string) *html.Node { if n.Type == html.ElementNode && n.Data == tagName { return n }; for c := n.FirstChild; c != nil; c = c.NextSibling { if result := findNode(c, tagName); result != nil { return result } }; return nil }
func getTitleText(n *html.Node) string { if n.FirstChild != nil && n.FirstChild.Type == html.TextNode { return strings.TrimSuffix(n.FirstChild.Data, " — ЖЖ") }; return "" }
func renderInnerNode(n *html.Node) (string, error) { var b bytes.Buffer; for c := n.FirstChild; c != nil; c = c.NextSibling { err := html.Render(&b, c); if err != nil { return "", err } }; return b.String(), nil }
func resolveURL(base *url.URL, href string) string { rel, err := url.Parse(href); if err != nil { return "" }; return base.ResolveReference(rel).String() }
func cleanURL(rawURL string) string { u, err := url.Parse(rawURL); if err != nil { return rawURL }; u.RawQuery = ""; u.Fragment = ""; return u.String() }