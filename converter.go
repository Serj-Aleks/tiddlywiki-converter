package tiddlywiki_converter

import (
	"fmt"
	"log" 
	"net/url"    
	"tiddlywiki-converter/blogger"
	"tiddlywiki-converter/hashnode"
	"tiddlywiki-converter/livejournal"
	"tiddlywiki-converter/tiddlywiki"
	"tiddlywiki-converter/wordpress"
	"tiddlywiki-converter/wikipedia"
)

func Convert(config map[string]string) ([]*tiddlywiki.Tiddler, error) {
	platform, ok := config["platform"]
	if !ok {
		return nil, fmt.Errorf("платформа не указана в конфигурации")
	}

	switch platform {
        
    case "hashnode":
        username := config["username"]
        host := config["host"]
        pageURL := config["url"]

    // Если указан URL, но не указан хост, извлекаем хост из URL
        if pageURL != "" && host == "" {
            parsedURL, err := url.Parse(pageURL)
            if err != nil {
                return nil, fmt.Errorf("некорректный URL для Hashnode: %w", err)
            }
            host = parsedURL.Host
            log.Printf("Извлечен хост из URL: %s", host)
        }

        if username == "" && host == "" {
            return nil, fmt.Errorf("для Hashnode необходимо указать --url, --host или --user")
        }
    
        return hashnode.ConvertFromAPI(username, host)
	
    case "wordpress":
		url := config["url"]
		xmlPath := config["xml_path"]

		if xmlPath != "" {
			log.Println("Вызываю конвертер WordPress для XML...")
			return wordpress.ConvertFromXMLFile(xmlPath)
		}
		
		if url != "" {
			log.Println("Вызываю конвертер WordPress для URL...")
			return wordpress.ConvertFromURL(url)
		}
		
		return nil, fmt.Errorf("для WordPress необходимо указать --url или --xml_path")
		
	case "blogger":
		apiKey := config["api_key"]
		blogID := config["blog_id"]
		blogURL := config["url"]

		if apiKey == "" {
			return nil, fmt.Errorf("для Blogger необходимо указать --api_key")
		}

		if blogURL != "" {
			return blogger.ConvertFromURL(apiKey, blogURL)
		} else if blogID != "" {
			return blogger.ConvertFromBlogID(apiKey, blogID)
		} else {
			return nil, fmt.Errorf("для Blogger необходимо указать --url или --blog_id")
		}

	case "livejournal":
		pageURL := config["url"]
		if pageURL == "" {
			return nil, fmt.Errorf("для LiveJournal необходимо указать --url")
		}
		log.Printf("Запускаем конвертацию LiveJournal для URL: %s", pageURL)
		return livejournal.ConvertFromURL(pageURL)
		
	// =========================================================================
	// ВОЗВРАЩАЕМ УДАЛЕННЫЙ КОД
	// =========================================================================
	case "wikipedia":
		pageURL, ok := config["url"]
		if !ok || pageURL == "" {
			return nil, fmt.Errorf("для Wikipedia необходимо указать --url")
		}
		log.Printf("Запускаем конвертацию Wikipedia для URL: %s", pageURL)
		return wikipedia.ConvertFromURL(pageURL)
	// =========================================================================

	default:
		return nil, fmt.Errorf("неизвестная или неподдерживаемая платформа: %s", platform)
	}
}