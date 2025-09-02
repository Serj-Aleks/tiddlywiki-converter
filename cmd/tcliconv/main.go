package main

import (
	"flag"
	"log"
	"strings"

	tiddlywiki_converter "tiddlywiki-converter"
	"tiddlywiki-converter/tiddlywiki"
)

func main() {
	platformPtr := flag.String("platform", "", "Платформа (hashnode, wordpress, blogger, wikipedia, livejournal)")
	urlPtr := flag.String("url", "", "URL для конвертации (WordPress, Blogger, Wikipedia, LiveJournal)")
	usernamePtr := flag.String("user", "", "Имя пользователя Hashnode")
	hostPtr := flag.String("host", "", "Кастомный домен блога Hashnode")
	xmlPathPtr := flag.String("xml_path", "", "Путь к XML-файлу экспорта WordPress")
	apiKeyPtr := flag.String("api_key", "", "API ключ для Blogger")
	blogIDPtr := flag.String("blog_id", "", "ID блога на Blogger")
	flag.Parse()

	config := map[string]string{
		"platform": *platformPtr,
		"url":      *urlPtr,
		"username": *usernamePtr,
		"host":     *hostPtr,
		"xml_path": *xmlPathPtr,
		"api_key":  *apiKeyPtr,
		"blog_id":  *blogIDPtr,
	}

	if config["platform"] == "" {
		log.Fatal("Ошибка: Укажите платформу (--platform)")
	}

	log.Printf("Запускаем конвертацию для платформы '%s'...", config["platform"])
    
    log.Println("main: Вызываем tiddlywiki-converter.Convert с config:", config["platform"], config["url"])
    
	tiddlers, err := tiddlywiki_converter.Convert(config)
	if err != nil {
		log.Fatalf("Ошибка конвертации: %v", err)
	}

	var baseName string
	if config["blog_id"] != "" {
		baseName = config["blog_id"]
	} else if config["url"] != "" {
		baseName = config["url"]
	} else if config["host"] != "" {
		baseName = config["host"]
	} else if config["username"] != "" {
		baseName = config["username"]
	} else {
		baseName = config["platform"]
	}
	baseName = strings.ReplaceAll(baseName, "http://", "")
	baseName = strings.ReplaceAll(baseName, "https://", "")
	baseName = strings.ReplaceAll(baseName, "/", "_")
	
	outputPath := baseName + "_import.html"
	// === ИСПРАВЛЕНИЕ: ПРАВИЛЬНЫЙ ПУТЬ К ШАБЛОНУ ===
	templatePath := "../../internal/template.html" 
	
	log.Printf("Сконвертировано %d тиддлеров.", len(tiddlers))
	log.Println("Генерация TiddlyWiki файла...")

	err = tiddlywiki.GenerateHTML(tiddlers, templatePath, outputPath)
	if err != nil {
		log.Fatalf("Ошибка при генерации HTML: %v", err)
	}

	log.Printf("Файл успешно создан: %s", outputPath)
}