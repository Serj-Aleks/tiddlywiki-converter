package wikipedia

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"tiddlywiki-converter/tiddlywiki"
)

// ProjectInfo содержит информацию о проекте Wikimedia (язык, домен, имя).
type ProjectInfo struct {
	Language    string
	Domain      string
	ProjectName string
}

// getProjectInfoFromURL извлекает информацию о проекте из URL страницы.
func getProjectInfoFromURL(pageURL string) (*ProjectInfo, error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("некорректный URL: %w", err)
	}
	hostParts := strings.Split(parsedURL.Host, ".")
	if len(hostParts) < 3 {
		return nil, fmt.Errorf("не удалось определить проект из хоста: %s. Ожидается формат 'lang.project.org'", parsedURL.Host)
	}
	return &ProjectInfo{
		Language:    hostParts[0],
		Domain:      parsedURL.Host,
		ProjectName: hostParts[1],
	}, nil
}

// --- ASON-ПАРСЕР (C-WIKI-v.6) ---

// hasClass проверяет, имеет ли узел один из указанных CSS-классов.
func hasClass(n *html.Node, classNames ...string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			classes := strings.Fields(attr.Val)
			for _, wanted := range classNames {
				for _, c := range classes {
					if c == wanted {
						return true
					}
				}
			}
		}
	}
	return false
}

// extractText рекурсивно извлекает весь видимый текст из узла и его потомков.
func extractText(n *html.Node) string {
	if n == nil {
		return ""
	}
	if hasClass(n, "navbar", "navbox-toggler", "mw-collapsible-toggle") || n.Data == "style" || n.Data == "script" {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type != html.ElementNode {
		return ""
	}
	var b bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(extractText(c))
	}
	return strings.TrimSpace(b.String())
}

// addSpaceIfNeeded добавляет пробел в builder, если это необходимо.
func addSpaceIfNeeded(b *strings.Builder) {
	if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") && !strings.HasSuffix(b.String(), "<b>[</b> ") {
		b.WriteString(" ")
	}
}

// findChildNode рекурсивно ищет первый дочерний узел.
func findChildNode(n *html.Node, tag, className string) *html.Node {
	if n == nil {
		return nil
	}
	var result *html.Node
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode {
			matchTag := tag == "" || node.Data == tag
			matchClass := className == "" || hasClass(node, className)
			if matchTag && matchClass {
				result = node
				return
			}
		}
		for c := node.FirstChild; c != nil && result == nil; c = c.NextSibling {
			// Не ищем внутри вложенных navbox'ов, чтобы не захватить чужой заголовок
			if hasClass(c, "navbox") {
				continue
			}
			f(c)
		}
	}
	f(n)
	return result
}

// renderTitleNode обрабатывает узел заголовка, корректно форматируя его.
// ВЕРСИЯ 3: Переписана для надежности и исправления ошибок форматирования.
func renderTitleNode(titleNode *html.Node, b *strings.Builder, projectInfo *ProjectInfo) {
	if titleNode == nil {
		return
	}

	cleanText := extractText(titleNode)
	if cleanText == "" {
		return
	}

	isMultiWord := len(strings.Fields(cleanText)) > 1
	formattedText := cleanText
	if isMultiWord {
		formattedText = `"` + cleanText + `"`
	}

	addSpaceIfNeeded(b)

	linkNode := findChildNode(titleNode, "a", "")
	if linkNode != nil {
		href := ""
		for _, attr := range linkNode.Attr {
			if attr.Key == "href" {
				href = attr.Val
			}
		}
		fullURL := fmt.Sprintf("https://%s%s", projectInfo.Domain, href)
		b.WriteString(fmt.Sprintf(`<b><a href="%s" target="_blank">%s</a></b>`, fullURL, formattedText))
	} else {
		b.WriteString(fmt.Sprintf(`<b>%s</b>`, formattedText))
	}
}

// asonFromNode - ФИНАЛЬНАЯ ВЕРСIЯ: Исправляет и ссылки, и теги.
func asonFromNode(node, rootNode *html.Node, b *strings.Builder, projectInfo *ProjectInfo, parentTiddlerTitle string, baseTag string) []*tiddlywiki.Tiddler {
	if node == nil {
		return nil
	}
	
	var createdTiddlers []*tiddlywiki.Tiddler

	// Игнорируем служебные узлы
	if hasClass(node, "navbar", "mw-collapsible-toggle") || node.Data == "style" || node.Data == "script" {
		return nil
	}
	
	if hasClass(node, "navbox-title") {
		isRootTitle := false
		for p := node; p != nil; p = p.Parent {
			if p == rootNode {
				isRootTitle = true
				break
			}
		}
		if isRootTitle {
			return nil
		}
	}

	// Обрабатываем конечные узлы и выходим
	if hasClass(node, "navbox-group") {
		renderTitleNode(node, b, projectInfo)
		return nil
	}
	if node.Type == html.ElementNode && node.Data == "a" {
		href := ""
		for _, attr := range node.Attr {
			if attr.Key == "href" {
				href = attr.Val
			}
		}
		if !strings.HasPrefix(href, "/wiki/Template:") && !strings.HasPrefix(href, "/wiki/Шаблон:") {
			text := extractText(node)
			if text != "" {
				addSpaceIfNeeded(b)
				isMultiWord := len(strings.Fields(text)) > 1
				fullURL := fmt.Sprintf("https://%s%s", projectInfo.Domain, href)
				if isMultiWord {
					b.WriteString(fmt.Sprintf(`<a href="%s" target="_blank">"%s"</a>`, fullURL, text))
				} else {
					b.WriteString(fmt.Sprintf(`<a href="%s" target="_blank">%s</a>`, fullURL, text))
				}
			}
		}
		return nil
	}
	if node.Type == html.TextNode {
		text := strings.TrimSpace(node.Data)
		if text != "" && text != "•" && text != "·" && text != "|" {
			addSpaceIfNeeded(b)
			b.WriteString(text)
		}
		return nil
	}

	isList := hasClass(node, "navbox-list", "navbox-abovebelow")
	if isList {
		addSpaceIfNeeded(b)
		b.WriteString("<b>[</b> ")
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if hasClass(c, "navbox-subgroup") && hasClass(c, "mw-collapsible") {
			var subTitleText string
			titleNode := findChildNode(c, "", "navbox-title")
			if titleNode != nil {
				subTitleText = extractText(titleNode)
			}
			if subTitleText == "" { subTitleText = "Вложенный шаблон" }

			subTiddlerTitle := parentTiddlerTitle + " / " + subTitleText

			addSpaceIfNeeded(b)
			// Правильная ссылка без кавычек
			b.WriteString(fmt.Sprintf(`<b>[[%s]]</b>`, subTiddlerTitle))

			var subBuilder strings.Builder
			// Передаем subTiddlerTitle как родительский для следующих уровней
			deeperTiddlers := asonFromNode(c, c, &subBuilder, projectInfo, subTiddlerTitle, baseTag)
			
			// --- НАЧАЛО ИЗМЕНЕНИЯ (та самая одна строка) ---
			// Тег создается на основе текущего, правильного parentTiddlerTitle, а не "протекшего".
			subTiddler := tiddlywiki.NewTiddler(
				subTiddlerTitle,
				strings.TrimSpace(subBuilder.String()),
				baseTag+" "+parentTiddlerTitle,
			)
			// --- КОНЕЦ ИЗМЕНЕНИЯ ---

			createdTiddlers = append(createdTiddlers, subTiddler)
			createdTiddlers = append(createdTiddlers, deeperTiddlers...)
			log.Printf("Создан тиддлер для вложенного шаблона: '%s'", subTiddlerTitle)

		} else {
			// Передаем ТОТ ЖЕ parentTiddlerTitle для узлов того же уровня
			deeperTiddlers := asonFromNode(c, rootNode, b, projectInfo, parentTiddlerTitle, baseTag)
			createdTiddlers = append(createdTiddlers, deeperTiddlers...)
		}
	}

	if isList {
		currentString := b.String()
		if strings.HasSuffix(currentString, " ") {
			b.Reset()
			b.WriteString(strings.TrimSuffix(currentString, " "))
		}
		b.WriteString(" <b>]</b>")
	}
	return createdTiddlers
}

// parseFragment преобразует строку HTML в узел для дальнейшей обработки.
func parseFragment(s string) *html.Node {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return nil
	}
	var body *html.Node
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "body" {
			body = n
			return
		}
		for c := n.FirstChild; c != nil && body == nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return body
}

// ConvertFromURL - адаптирована для работы с новой asonFromNode, которая возвращает тиддлеры.
func ConvertFromURL(pageURL string) ([]*tiddlywiki.Tiddler, error) {
	projectInfo, err := getProjectInfoFromURL(pageURL)
	if err != nil {
		return nil, err
	}
	log.Printf("Начинаем конвертацию страницы %s: %s", projectInfo.ProjectName, pageURL)

	articleTitle, err := getArticleTitleFromURL(pageURL)
	if err != nil {
		return nil, err
	}
	log.Printf("Название статьи: %s", articleTitle)

	htmlContent, err := fetchArticleHTML(articleTitle, projectInfo.Domain)
	if err != nil {
		return nil, err
	}
	log.Printf("Получено %d байт HTML-кода.", len(htmlContent))

	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга основного HTML: %w", err)
	}

	pageTitlePattern := `(?s)<h1.*?>(.*?)</h1>`
	pageTitle := extractFirstMatch(htmlContent, pageTitlePattern)
	if pageTitle == "" {
		pageTitle = articleTitle
	}
	pageTitle = regexp.MustCompile("<[^>]*>").ReplaceAllString(pageTitle, "")
	log.Printf("Заголовок страницы: %s", pageTitle)

	var tiddlers []*tiddlywiki.Tiddler
	importTag := projectInfo.ProjectName + "-" + strings.ToLower(articleTitle)
	baseTag := projectInfo.ProjectName + "-шаблон " + importTag

	var allNavboxes []*html.Node
	var findNavboxes func(*html.Node)
	findNavboxes = func(n *html.Node) {
		if hasClass(n, "navbox") {
			allNavboxes = append(allNavboxes, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findNavboxes(c)
		}
	}
	findNavboxes(doc)

	var rootNavboxes []*html.Node
	// ... (код определения rootNavboxes остается прежним)
	isNested := make(map[*html.Node]bool)
	for _, outer := range allNavboxes {
		for _, inner := range allNavboxes {
			if outer == inner {
				continue
			}
			var isDescendant func(*html.Node) bool
			isDescendant = func(n *html.Node) bool {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c == inner || isDescendant(c) {
						return true
					}
				}
				return false
			}
			if isDescendant(outer) {
				isNested[inner] = true
			}
		}
	}
	for _, navbox := range allNavboxes {
		if !isNested[navbox] {
			rootNavboxes = append(rootNavboxes, navbox)
		}
	}


	if len(rootNavboxes) > 0 {
		for i, navboxNode := range rootNavboxes {
			var navboxTitle string
			titleNode := findChildNode(navboxNode, "", "navbox-title")
			if titleNode != nil {
				navboxTitle = extractText(titleNode)
			}
			if navboxTitle == "" {
				navboxTitle = fmt.Sprintf("Нижний шаблон %d", i+1)
			}

			tiddlerTitle := pageTitle + ":" + navboxTitle

			var b strings.Builder
			// asonFromNode теперь возвращает созданные вложенные тиддлеры
			createdTiddlers := asonFromNode(navboxNode, navboxNode, &b, projectInfo, tiddlerTitle, baseTag)
			tiddlers = append(tiddlers, createdTiddlers...) // Добавляем их в общий список

			asonContent := strings.TrimSpace(b.String())
			if asonContent == "" {
				log.Printf("ПРЕДУПРЕЖДЕНИЕ: Для шаблона '%s' не сгенерировано содержимое ASON. Пропускаем.", navboxTitle)
				if navboxNode.Parent != nil {
					navboxNode.Parent.RemoveChild(navboxNode)
				}
				continue
			}

			navboxTiddler := tiddlywiki.NewTiddler(tiddlerTitle, asonContent, baseTag)
			tiddlers = append(tiddlers, navboxTiddler)
			log.Printf("Создан тиддлер для корневого шаблона: '%s'", tiddlerTitle)

			if navboxNode.Parent != nil {
				navboxNode.Parent.RemoveChild(navboxNode)
			}
		}
	}

	var renderedHTML bytes.Buffer
	html.Render(&renderedHTML, doc)
	htmlContent = renderedHTML.String()

	// ... остальная часть функции без изменений ...
	infoboxPattern := `(?s)(<table class="infobox.*?</table>)`
	infoboxHTML, fullInfoboxMatch := extractFirstMatchWithFull(htmlContent, infoboxPattern)
	if infoboxHTML != "" {
		cleanedInfobox := cleanupHTML(infoboxHTML, pageTitle, projectInfo, nil)
		infoboxTiddler := tiddlywiki.NewTiddler(
			pageTitle+": Шаблон-карточка",
			cleanedInfobox,
			projectInfo.ProjectName+"-шаблон "+importTag,
		)
		tiddlers = append(tiddlers, infoboxTiddler)
		htmlContent = strings.Replace(htmlContent, fullInfoboxMatch, "", 1)
	}

	relatedProjectPattern := `(?s)(<table.*?class="ts-Родственный_проект.*?>.*?</table>)`
	relatedProjectHTML, fullRelatedMatch := extractFirstMatchWithFull(htmlContent, relatedProjectPattern)
	if relatedProjectHTML != "" {
		cleanedRelated := cleanupHTML(relatedProjectHTML, pageTitle, projectInfo, nil)
		relatedProjectTiddler := tiddlywiki.NewTiddler(
			pageTitle+": Родственные проекты",
			cleanedRelated,
			projectInfo.ProjectName+"-ссылки "+importTag,
		)
		tiddlers = append(tiddlers, relatedProjectTiddler)
		htmlContent = strings.Replace(htmlContent, fullRelatedMatch, "", 1)
	}

	var notesSectionTitle string
	notesKeywords := []string{"Примечания", "References", "Сноски", "Notes"}
	headerFindRegex := regexp.MustCompile(`(?s)<h[2-4].*?>(.*?)</h[2-4]>`)
	allHeadersForNotes := headerFindRegex.FindAllStringSubmatch(htmlContent, -1)
	for _, header := range allHeadersForNotes {
		title := strings.TrimSpace(regexp.MustCompile("<[^>]*>").ReplaceAllString(header[1], ""))
		for _, keyword := range notesKeywords {
			if strings.EqualFold(title, keyword) {
				notesSectionTitle = title
				break
			}
		}
		if notesSectionTitle != "" {
			break
		}
	}

	htmlContent = cleanupHTML(htmlContent, pageTitle, projectInfo, &notesSectionTitle)

	headerPattern := `(?s)(<(h[2-4]).*?>.*?/h\d>)`
	headerRegex := regexp.MustCompile(headerPattern)
	allHeaders := headerRegex.FindAllStringSubmatch(htmlContent, -1)
	splitContent := headerRegex.Split(htmlContent, -1)

	introHTML := strings.TrimSpace(splitContent[0])
	introHTML += fmt.Sprintf(`<p><br><i>Источник: <a href="%s" target="_blank" rel="noopener noreferrer">%s</a></i></p>`, pageURL, pageURL)
	mainTiddler := tiddlywiki.NewTiddler(
		pageTitle,
		introHTML,
		projectInfo.ProjectName+"-статья "+importTag,
	)
	mainTiddler.Fields["source-url"] = pageURL
	tiddlers = append(tiddlers, mainTiddler)

	var currentH2, currentH3 string
	if len(splitContent) > 1 {
		editSectionRegex := regexp.MustCompile(`(?s)^\s*<span class="[^"]*?mw-editsection[^"]*?">.*?</span></div>`)
		for i, sectionContent := range splitContent[1:] {
			headerHTML := allHeaders[i][1]
			headerTag := allHeaders[i][2]
			rawSectionTitle := regexp.MustCompile("<[^>]*>").ReplaceAllString(headerHTML, "")
			sectionTitle := strings.TrimSpace(rawSectionTitle)
			finalSectionContent := strings.TrimSpace(editSectionRegex.ReplaceAllString(sectionContent, ""))

			if notesSectionTitle != "" && sectionTitle == notesSectionTitle {
				backlinkSpanRegex := regexp.MustCompile(`(?s)<span class="mw-cite-backlink">.*?</span>`)
				finalSectionContent = backlinkSpanRegex.ReplaceAllString(finalSectionContent, "")
			}

			var tiddlerTitle string
			switch headerTag {
			case "h2":
				currentH2 = sectionTitle
				tiddlerTitle = pageTitle + ": " + currentH2
				currentH3 = ""
			case "h3":
				currentH3 = sectionTitle
				if currentH2 != "" {
					tiddlerTitle = pageTitle + ": " + currentH2 + " / " + currentH3
				} else {
					tiddlerTitle = pageTitle + ": " + currentH3
				}
			case "h4":
				if currentH2 != "" && currentH3 != "" {
					tiddlerTitle = pageTitle + ": " + currentH2 + " / " + currentH3 + " / " + sectionTitle
				} else if currentH2 != "" {
					tiddlerTitle = pageTitle + ": " + currentH2 + " / " + sectionTitle
				} else {
					tiddlerTitle = pageTitle + ": " + sectionTitle
				}
			}

			sectionTiddler := tiddlywiki.NewTiddler(
				tiddlerTitle,
				finalSectionContent,
				projectInfo.ProjectName+"-раздел "+importTag,
			)
			tiddlers = append(tiddlers, sectionTiddler)
		}
	}

	categories, err := fetchCategories(articleTitle, projectInfo.Domain)
	if err != nil {
		log.Printf("Не удалось получить категории: %v", err)
	} else if len(categories) > 0 {
		var catLinks []string
		for _, cat := range categories {
			catTitle := strings.TrimPrefix(cat, "Категория:")
			catTitle = strings.TrimPrefix(catTitle, "Category:")
			catURL := fmt.Sprintf("https://%s/wiki/%s", projectInfo.Domain, url.QueryEscape(cat))
			catLinks = append(catLinks, fmt.Sprintf(`<li><a href="%s" target="_blank" rel="noopener noreferrer">%s</a></li>`, catURL, catTitle))
		}
		catHTML := "<ul>\n" + strings.Join(catLinks, "\n") + "\n</ul>"
		catTiddler := tiddlywiki.NewTiddler(
			pageTitle+": Категории",
			catHTML,
			projectInfo.ProjectName+"-категории "+importTag,
		)
		tiddlers = append(tiddlers, catTiddler)
	}

	return tiddlers, nil
}

func getArticleTitleFromURL(pageURL string) (string, error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("некорректный URL: %w", err)
	}
	const prefix = "/wiki/"
	path := parsedURL.Path
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("URL path не содержит '%s': %s", prefix, path)
	}
	encodedTitle := path[len(prefix):]
	decodedTitle, err := url.PathUnescape(encodedTitle)
	if err != nil {
		return "", fmt.Errorf("не удалось раскодировать название статьи '%s': %w", encodedTitle, err)
	}
	return decodedTitle, nil
}

func fetchArticleHTML(articleTitle, domain string) (string, error) {
	apiURL := fmt.Sprintf("https://%s/w/api.php?action=parse&page=%s&prop=text&format=json&disabletoc=true", domain, url.QueryEscape(articleTitle))
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("ошибка при запросе к API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API вернул статус: %s", resp.Status)
	}
	var result struct {
		Parse struct {
			Text struct {
				Content string `json:"*"`
			} `json:"text"`
		} `json:"parse"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ошибка при декодировании JSON-ответа от API: %w", err)
	}
	if result.Parse.Text.Content == "" {
		return "", fmt.Errorf("не удалось найти HTML-контент в ответе от API")
	}
	return result.Parse.Text.Content, nil
}

func fetchCategories(articleTitle, domain string) ([]string, error) {
	apiURL := fmt.Sprintf("https://%s/w/api.php?action=query&prop=categories&titles=%s&format=json&cllimit=max&clshow=!hidden", domain, url.QueryEscape(articleTitle))
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе категорий: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Query struct {
			Pages map[string]struct {
				Categories []struct {
					Title string `json:"title"`
				} `json:"categories"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	var categories []string
	for _, page := range result.Query.Pages {
		for _, cat := range page.Categories {
			categories = append(categories, cat.Title)
		}
	}
	return categories, nil
}

func extractFirstMatchWithFull(html, pattern string) (string, string) {
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1], matches[0]
	}
	if len(matches) == 1 {
		return matches[0], matches[0]
	}
	return "", ""
}

func extractFirstMatch(html, pattern string) string {
	content, _ := extractFirstMatchWithFull(html, pattern)
	return content
}

func cleanupHTML(html, pageTitle string, projectInfo *ProjectInfo, notesSectionTitle *string) string {
	cleaned := html
	cleaned = regexp.MustCompile(`(?s)<html><head></head><body>(.*)</body></html>`).ReplaceAllString(cleaned, "$1")
	cleaned = regexp.MustCompile(`(?s)<table class="mbox.*?</table>`).ReplaceAllString(cleaned, "")
	cleaned = regexp.MustCompile(`(?s)<span class="wikidata-editlink">.*?</span>`).ReplaceAllString(cleaned, "")
	if notesSectionTitle != nil && *notesSectionTitle != "" {
		tiddlerTitle := pageTitle + ": " + *notesSectionTitle
		supRegex := regexp.MustCompile(`(?s)<sup id="cite_ref-[^"]+" class="reference"><a href="#[^"]+">.*?</a></sup>`)
		cleaned = supRegex.ReplaceAllString(cleaned, fmt.Sprintf(`[[*|%s]]`, tiddlerTitle))
	}
	cleaned = strings.ReplaceAll(cleaned, `src="//`, `src="https://`)
	cleaned = strings.ReplaceAll(cleaned, `srcset="//`, `srcset="https://`)
	linkRegex := regexp.MustCompile(`<a\s+([^>]+)>`)
	cleaned = linkRegex.ReplaceAllStringFunc(cleaned, func(match string) string {
		hrefRegex := regexp.MustCompile(`href="/wiki/([^"]+)"`)
		newMatch := hrefRegex.ReplaceAllString(match, fmt.Sprintf(`href="https://%s/wiki/$1"`, projectInfo.Domain))
		if !strings.Contains(newMatch, "target=") {
			newMatch = strings.Replace(newMatch, ">", ` target="_blank" rel="noopener noreferrer">`, 1)
		}
		return newMatch
	})
	cleaned = regexp.MustCompile(`<p class="mw-empty-elt">\s*</p>`).ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)
	if strings.HasSuffix(cleaned, "</div>") {
		cleaned = strings.TrimSuffix(cleaned, "</div>")
		cleaned = strings.TrimSpace(cleaned)
	}
	return cleaned
}