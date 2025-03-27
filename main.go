package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/dromara/carbon/v2"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type App struct {
	Name        string
	Description string
	SourceURL   string
	License     string
	Language    string
	Tags        []string
	Stars       int
	CreatedAt   string
	FirstCommit string
	LastCommit  string
}

func loadLabels(locale string) (map[string]string, error) {
	path := fmt.Sprintf("./i18n/%s.json", locale)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var labels map[string]string
	err = json.Unmarshal(data, &labels)
	if err != nil {
		return nil, err
	}
	return labels, nil
}

func GenerateHandle(name string) string {
	handle := strings.ReplaceAll(name, ".", "-")
	handle = strings.ReplaceAll(handle, " ", "-")
	handle = strings.ReplaceAll(handle, "_", "-")

	re := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	handle = re.ReplaceAllString(handle, `${1}${2}`)

	handle = strings.ToLower(handle)

	return handle
}

func GetRepoHoster(repoURL string) string {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "Unknown"
	}

	host := u.Host
	switch {
	case strings.Contains(host, "github.com"):
		return "GitHub"
	case strings.Contains(host, "gitlab.com"):
		return "GitLab"
	case strings.Contains(host, "codeberg.org"):
		return "Codeberg"
	case strings.Contains(host, "sr.ht"):
		return "SourceHut"
	default:
		return "Unknown"
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	dsn := "postgres://" + dbUser + ":" + dbPassword + "@localhost:5432/" + dbName + "?sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	r := gin.Default()
	r.Static("/static", "./static")

	r.GET("/", func(c *gin.Context) {
		locale := c.DefaultQuery("lang", "en")
		carbon.SetLocale(locale)

		labels, err := loadLabels(locale)
		if err != nil {
			log.Println("Could not load language file, falling back to English.", err)
			labels, _ = loadLabels("en")
		}

		currentQuery := c.Request.URL.Query()
		currentQuery.Del("lang")
		baseURL := c.Request.URL.Path + "?" + currentQuery.Encode()
		if currentQuery.Encode() != "" {
			baseURL += "&"
		}

		sortParam := c.Query("sort")
		tagFilter := c.Query("tags")
		keyword := c.Query("q")

		tags := []string{}
		if tagFilter != "" {
			tags = strings.Split(tagFilter, ",")
		}

		query := "SELECT id, name, description, source_url, license, language, stars, created_at, first_commit, last_commit FROM apps"
		conditions := []string{}
		args := []interface{}{}
		argIdx := 1

		if keyword != "" {
			kwCond := []string{
				fmt.Sprintf("name ILIKE '%%' || $%d || '%%'", argIdx),
				fmt.Sprintf("description ILIKE '%%' || $%d || '%%'", argIdx),
				fmt.Sprintf("maintainer ILIKE '%%' || $%d || '%%'", argIdx),
				fmt.Sprintf("license ILIKE '%%' || $%d || '%%'", argIdx),
				fmt.Sprintf("country ILIKE '%%' || $%d || '%%'", argIdx),
				fmt.Sprintf("language ILIKE '%%' || $%d || '%%'", argIdx),
				fmt.Sprintf("id IN (SELECT app_id FROM app_tags WHERE tag ILIKE '%%' || $%d || '%%')", argIdx),
			}
			conditions = append(conditions, "("+strings.Join(kwCond, " OR ")+")")
			args = append(args, keyword)
			argIdx++
		}

		if len(tags) > 0 {
			placeholders := []string{}
			for _, tag := range tags {
				args = append(args, strings.TrimSpace(tag))
				placeholders = append(placeholders, fmt.Sprintf("$%d", argIdx))
				argIdx++
			}
			tagSub := fmt.Sprintf(`id IN (
			  SELECT app_id FROM app_tags
			  WHERE tag IN (%s)
			  GROUP BY app_id
			  HAVING COUNT(DISTINCT tag) = %d
			)`, strings.Join(placeholders, ","), len(tags))
			conditions = append(conditions, tagSub)
		}

		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()

		apps := []App{}
		for rows.Next() {
			var a App
			var id string
			if err := rows.Scan(&id, &a.Name, &a.Description, &a.SourceURL, &a.License, &a.Language, &a.Stars, &a.CreatedAt, &a.FirstCommit, &a.LastCommit); err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}

			tagRows, err := db.Query("SELECT tag FROM app_tags WHERE app_id = $1", id)
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			defer tagRows.Close()

			tagList := []string{}
			for tagRows.Next() {
				var tag string
				if err := tagRows.Scan(&tag); err != nil {
					c.String(http.StatusInternalServerError, err.Error())
					return
				}
				tagList = append(tagList, tag)
			}
			a.Tags = tagList

			apps = append(apps, a)
		}

		switch sortParam {
		case "stars":
			sort.Slice(apps, func(i, j int) bool { return apps[i].Stars > apps[j].Stars })
		case "activity":
			sort.Slice(apps, func(i, j int) bool { return apps[i].LastCommit > apps[j].LastCommit })
		case "recently_added":
			sort.Slice(apps, func(i, j int) bool { return apps[i].CreatedAt > apps[j].CreatedAt })
		case "age_asc":
			sort.Slice(apps, func(i, j int) bool { return apps[i].FirstCommit < apps[j].FirstCommit })
		case "age_desc":
			sort.Slice(apps, func(i, j int) bool { return apps[i].FirstCommit > apps[j].FirstCommit })
		default:
			sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
		}

		html := `<!DOCTYPE html><html><head><meta charset="UTF-8"><title>` + labels["title"] + `</title>
		<style>
			body {
				font-family: system-ui, sans-serif;
				margin: 2rem auto;
				max-width: 800px;
				line-height: 1.6;
				padding: 0 1rem;
				background: #f9fafb;
				color: #111;
			}
			
			h1 {
				font-size: 1.8rem;
				margin-bottom: 1.5rem;
			}
			
			form {
				background: #fff;
				padding: 1rem;
				border-radius: 8px;
				margin-bottom: 2rem;
				box-shadow: 0 1px 4px rgba(0,0,0,0.05);
			}
			
			form label {
				display: block;
				margin-top: 0.8rem;
				font-weight: 600;
			}
			
			form input[type="text"],
			form select {
				width: 100%;
				padding: 0.5rem;
				font-size: 1rem;
				margin-top: 0.25rem;
				border: 1px solid #ccc;
				border-radius: 4px;
			}
			
			form input[type="submit"] {
				margin-top: 1rem;
				background: #2563eb;
				color: white;
				padding: 0.6rem 1.2rem;
				border: none;
				border-radius: 4px;
				cursor: pointer;
			}
			
			form input[type="submit"]:hover {
				background: #1d4ed8;
			}
			
			.language-switch {
				float: right;
				font-size: 0.9rem;
				margin-top: -2rem;
			}
			
			.language-switch a {
				color: #2563eb;
				text-decoration: none;
				margin: 0 0.25rem;
			}
			
			.language-switch a:hover {
				text-decoration: underline;
			}
			
			ul.app-list {
				list-style: none;
				padding: 0;
				margin: 0;
			}
			
			ul.app-list li {
				display: flex;
				flex-direction: row;
				align-items: flex-start;
				gap: 1rem;
				background: white;
				margin-bottom: 1.5rem;
				padding: 1rem 1.2rem;
				border-radius: 12px;
				box-shadow: 0 4px 12px rgba(0,0,0,0.06);
				transition: transform 0.2s ease;
			}
			
			ul.app-list li:hover {
				transform: translateY(-2px);
			}
			
			ul.app-list li img {
				width: 48px;
				height: 48px;
				border-radius: 8px;
				flex-shrink: 0;
			}
			
			.app-content {
				flex: 1;
			}
			
			.app-title {
				font-size: 1.2rem;
				margin: 0;
				font-weight: 600;
			}
			
			.app-title a {
				text-decoration: none;
				color: #111;
			}
			
			.app-title a:hover {
				text-decoration: underline;
			}
			
			.meta {
				font-size: 0.9rem;
				color: #444;
				margin-top: 0.2rem;
			}
			
			.tags {
				margin-top: 0.5rem;
				font-size: 0.85rem;
				color: #666;
			}
			
			.tags strong {
				color: #333;
			}

			span.tag {
				display: inline-block;
				background-color: #e0e7ff;
				color: #3730a3;
				font-size: 0.75rem;
				padding: 0.2rem 0.5rem;
				margin: 0.2rem 0.2rem 0 0;
				border-radius: 999px;
				font-weight: 500;
				white-space: nowrap;
			}
		</style>

		</head>
		<body>`

		html += `<div style="float:right;">
		<a href="` + baseURL + `lang=en">English</a> |
		<a href="` + baseURL + `lang=de">Deutsch</a> |
		<a href="` + baseURL + `lang=fr">Français</a>
		</div>`

		html += `<h1>` + labels["title"] + `</h1>
		<form method="get">
		<label for="q">` + labels["search"] + `</label>
		<input type="text" name="q" placeholder="` + labels["search_placeholder"] + `" value="` + keyword + `">
		<label for="tags">` + labels["filter_tags"] + `</label>
		<input type="text" name="tags" placeholder="` + labels["filter_tags_placeholder"] + `" value="` + tagFilter + `">
		<label for="sort">` + labels["sort_by"] + `</label>
		<select name="sort" onchange="this.form.submit()">`

		sortOptions := []struct {
			Value string
			Label string
		}{
			{"alphabetical", labels["sort_alphabetical"]},
			{"stars", labels["sort_stars"]},
			{"activity", labels["sort_activity"]},
			{"recently_added", labels["sort_recently_added"]},
			{"age_asc", labels["sort_age_asc"]},
			{"age_desc", labels["sort_age_desc"]},
		}

		for _, opt := range sortOptions {
			selected := ""
			if sortParam == opt.Value || (sortParam == "" && opt.Value == "alphabetical") {
				selected = " selected"
			}
			html += fmt.Sprintf("<option value=\"%s\"%s>%s</option>", opt.Value, selected, opt.Label)
		}

		html += `</select>
		<input type="hidden" name="lang" value="` + locale + `">
		<input type="submit" value="` + labels["apply"] + `">
		</form>`

		html += fmt.Sprintf("<p><strong>%d "+labels["results"]+"</strong></p><ul class=\"app-list\">", len(apps))

		for _, app := range apps {
			html += "<li>"
			html += "<img src=\"/static/icons/" + GenerateHandle(app.Name) + ".webp\" width=32 height=32 alt=\"" + GenerateHandle(app.Name) + "\">"
			html += "<div class=\"app-content\">"
			html += "<p class=\"app-title\">" + app.Name + "</p>"
			html += "<p>" + app.Description + "</p>"
			if len(app.Tags) > 0 {
				html += "<div>Tags: "
				for _, tag := range app.Tags {
					html += "<span class=\"tag\">" + tag + "</span>"
				}
				html += "</div>"
			}
			html += "<p>"
			html += labels["source"] + " <a href=\"" + app.SourceURL + "\">" + GetRepoHoster(app.SourceURL) + "</a>"
			html += " ⭐ " + itoa(app.Stars) + "<br/>"
			html += labels["license"] + " " + app.License + "<br/>"
			html += labels["language"] + " " + app.Language + "<br/>"
			last_activity := carbon.Parse(app.LastCommit)
			html += labels["last_activity"] + " <span title=\"" + app.LastCommit + "\">" + last_activity.DiffForHumans() + "</span><br/>"
			html += "</p>"
			html += "</div>"
			html += "</li>"
		}
		html += "</ul></body></html>"
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
	})

	r.Run(":8080")
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
