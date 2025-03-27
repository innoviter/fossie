package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"

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

	r.GET("/", func(c *gin.Context) {
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

		html := "<!DOCTYPE html><html><head><meta charset=\"UTF-8\"><title>FOSSIE – Open Source Software Index Europe</title></head><body>"
		html += `<h1>Open Source Software Index Europe (FOSSIE)</h1>
		<form method="get">
		<label for="q">Search:</label>
		<input type="text" name="q" placeholder="e.g. Germany cloud" value="` + keyword + `">
		<label for="tags">Filter by tags:</label>
		<input type="text" name="tags" placeholder="e.g. cloud,filesharing" value="` + tagFilter + `">
		<label for="sort">Sort by:</label>
		<select name="sort" onchange="this.form.submit()">`

		sortOptions := []struct {
			Value string
			Label string
		}{
			{"alphabetical", "Alphabetical"},
			{"stars", "Stars"},
			{"activity", "Last Activity"},
			{"recently_added", "Recently Added"},
			{"age_asc", "Age (Ascending)"},
			{"age_desc", "Age (Descending)"},
		}

		for _, opt := range sortOptions {
			selected := ""
			if sortParam == opt.Value || (sortParam == "" && opt.Value == "alphabetical") {
				selected = " selected"
			}
			html += fmt.Sprintf("<option value=\"%s\"%s>%s</option>", opt.Value, selected, opt.Label)
		}

		html += `</select>
		<input type="submit" value="Apply">
		</form>`

		html += fmt.Sprintf("<p><strong>%d result(s)</strong></p><ul>", len(apps))

		for _, app := range apps {
			html += "<li><strong>" + app.Name + "</strong><br>"
			html += "<em>" + app.Language + "</em> – " + app.License + " – ⭐ " + itoa(app.Stars) + "<br>"
			html += "<a href=\"" + app.SourceURL + "\">Source</a><br>"
			html += "<p>" + app.Description + "</p>"
			if len(app.Tags) > 0 {
				html += "<p><strong>Tags:</strong> " + strings.Join(app.Tags, ", ") + "</p>"
			}
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
