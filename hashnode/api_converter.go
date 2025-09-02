package hashnode

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/gomarkdown/markdown"
	"github.com/shurcooL/graphql"
	"tiddlywiki-converter/tiddlywiki"
)

// ИСПРАВЛЕННАЯ ВЕРСИЯ
type PublicationGQL struct {
	Posts struct {
		PageInfo struct {
			EndCursor   graphql.String
			HasNextPage graphql.Boolean
		}
		Edges []struct {
			Node struct {
				Title       graphql.String
				Slug        graphql.String
				Content     struct{ Markdown graphql.String }
				PublishedAt graphql.String
				Tags        []struct {
					Name graphql.String
					Slug graphql.String
				}
				Comments struct {
					Edges []struct{ Node CommentGQL }
				} `graphql:"comments(first: 50)"`
			}
		}
	} `graphql:"posts(first: $first, after: $after)"`
}

type ReplyGQL struct {
	Author    struct{ Name graphql.String }
	Content   struct{ Text graphql.String }
	DateAdded graphql.String
}

type CommentGQL struct {
	Author    struct{ Name graphql.String }
	Content   struct{ Text graphql.String }
	DateAdded graphql.String
	Replies   struct {
		Edges []struct{ Node ReplyGQL }
	} `graphql:"replies(first: 50)"`
}

// Запрос для получения постов по хосту (основной рабочий запрос)
type publicationByHostQuery struct {
	Publication PublicationGQL `graphql:"publication(host: $host)"`
}

// Запрос для получения хоста по имени пользователя (используется один раз)
type userHostQuery struct {
	User struct {
		Publications struct {
			Edges []struct {
				Node struct {
					Host graphql.String
				}
			}
		} `graphql:"publications(first: 1)"`
	} `graphql:"user(username: $username)"`
}

// processComments остается без изменений
func processComments(commentEdges []struct{ Node CommentGQL }, postTitle, postSlug string, tiddlers *[]*tiddlywiki.Tiddler) {
	for _, commentEdge := range commentEdges {
		comment := commentEdge.Node
		author := string(comment.Author.Name)
		commentText := fmt.Sprintf("<blockquote>%s</blockquote>\n\n''- %s''", string(comment.Content.Text), author)
		createdComm, _ := time.Parse(time.RFC3339, string(comment.DateAdded))
		tiddlyCommTime := createdComm.UTC().Format(tiddlywiki.TiddlyTimeFormat)

		commentTiddler := tiddlywiki.NewTiddler(
			fmt.Sprintf("Комментарий от %s к посту «%s»", author, postTitle),
			commentText, "comment",
		)
		commentTiddler.Created = tiddlyCommTime
		commentTiddler.Modified = tiddlyCommTime
		commentTiddler.Fields["parent-post"] = postSlug
		*tiddlers = append(*tiddlers, commentTiddler)

		for _, replyEdge := range comment.Replies.Edges {
			reply := replyEdge.Node
			replyAuthor := string(reply.Author.Name)
			replyText := fmt.Sprintf("<blockquote>%s</blockquote>\n\n''- %s''", string(reply.Content.Text), replyAuthor)
			createdReply, _ := time.Parse(time.RFC3339, string(reply.DateAdded))
			tiddlyReplyTime := createdReply.UTC().Format(tiddlywiki.TiddlyTimeFormat)

			replyTiddler := tiddlywiki.NewTiddler(
				fmt.Sprintf("Ответ от %s на комментарий к посту «%s»", replyAuthor, postTitle),
				replyText, "comment reply",
			)
			replyTiddler.Created = tiddlyReplyTime
			replyTiddler.Modified = tiddlyReplyTime
			replyTiddler.Fields["parent-post"] = postSlug
			*tiddlers = append(*tiddlers, replyTiddler)
		}
	}
}

// --- ИЗМЕНЕНИЕ: Полностью переписанная функция ConvertFromAPI ---
func ConvertFromAPI(username, host string) ([]*tiddlywiki.Tiddler, error) {
	client := graphql.NewClient("https://gql.hashnode.com/", nil)
	var allTiddlers []*tiddlywiki.Tiddler
	
	var publicationHost string

	// Шаг 1: Определяем хост блога.
	if host != "" {
		publicationHost = host
		log.Printf("Используем предоставленный хост: %s", publicationHost)
	} else if username != "" {
		log.Printf("Хост не предоставлен, ищем публикацию для пользователя: %s", username)
		var query userHostQuery
		variables := map[string]interface{}{"username": graphql.String(username)}
		err := client.Query(context.Background(), &query, variables)
		if err != nil {
			return nil, fmt.Errorf("ошибка при поиске публикации пользователя '%s': %w", username, err)
		}
		if len(query.User.Publications.Edges) == 0 {
			return nil, fmt.Errorf("у пользователя '%s' не найдено публикаций", username)
		}
		publicationHost = string(query.User.Publications.Edges[0].Node.Host)
		log.Printf("Найдена публикация с хостом: %s", publicationHost)
	} else {
		// Эта проверка дублируется в converter.go, но так надежнее
		return nil, fmt.Errorf("необходимо указать имя пользователя или хост")
	}

	// Шаг 2: Запускаем цикл пагинации, используя только хост.
	hasNextPage := true
	cursor := (*graphql.String)(nil)

	for hasNextPage {
		variables := map[string]interface{}{
			"host":  graphql.String(publicationHost),
			"first": graphql.Int(20),
			"after": cursor,
		}
		var query publicationByHostQuery
		err := client.Query(context.Background(), &query, variables)
		if err != nil {
			return nil, fmt.Errorf("ошибка при получении постов с хоста '%s': %w", publicationHost, err)
		}
		
		publication := query.Publication

		if len(publication.Posts.Edges) == 0 && len(allTiddlers) == 0 {
			return nil, fmt.Errorf("на хосте '%s' не найдено постов", publicationHost)
		}

		for _, edge := range publication.Posts.Edges {
			post := edge.Node
			htmlContent := string(markdown.ToHTML([]byte(post.Content.Markdown), nil, nil))
			postSlug := string(post.Slug)
			postTitle := string(post.Title)
			
			var postTags []string
			for _, tag := range post.Tags {
				postTags = append(postTags, SanitizeTag(string(tag.Name)))
			}
			tagsString := strings.Join(postTags, " ")

			created, _ := time.Parse(time.RFC3339, string(post.PublishedAt))
			tiddlyTime := created.UTC().Format(tiddlywiki.TiddlyTimeFormat)
			
			postTiddler := tiddlywiki.NewTiddler(postTitle, htmlContent, tagsString)
			postTiddler.Created = tiddlyTime
			postTiddler.Modified = tiddlyTime
			postTiddler.Fields["post-slug"] = postSlug
			
			// ДОБАВЛЕНО: Ссылка на источник
			sourceURL := fmt.Sprintf("https://%s/%s", publicationHost, postSlug)
			postTiddler.Fields["source-url"] = sourceURL
			
			allTiddlers = append(allTiddlers, postTiddler)

			processComments(post.Comments.Edges, postTitle, postSlug, &allTiddlers)
		}
		
		hasNextPage = bool(publication.Posts.PageInfo.HasNextPage)
		cursor = &publication.Posts.PageInfo.EndCursor
		log.Printf("Загружено %d постов, следующая страница: %v", len(publication.Posts.Edges), hasNextPage)
	}

	// ДОБАВЛЕНО: Создание системных тиддлеров для заголовка
	siteTitleTiddler := tiddlywiki.NewTiddler("$:/SiteTitle", "Hashnode", "")
	siteSubtitleTiddler := tiddlywiki.NewTiddler("$:/SiteSubtitle", publicationHost, "")
	allTiddlers = append(allTiddlers, siteTitleTiddler, siteSubtitleTiddler)

	return allTiddlers, nil
}

// SanitizeTag остается без изменений
func SanitizeTag(tag string) string {
	tag = strings.ReplaceAll(tag, " ", "-")
	reg := regexp.MustCompile("[^a-zA-Z0-9-]+")
	return reg.ReplaceAllString(tag, "")
}