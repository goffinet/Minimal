package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var configuration map[string]interface{}

func mustache(template string, context map[string]interface{}, partials interface{}) string {
	partialRegex := regexp.MustCompile(`\{\{>\s*([-_\/\.\w]+)\s*\}\}`)
	template = partialRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := partialRegex.FindStringSubmatch(match)[1]
		value := match
		if f, ok := partials.(func(string) string); ok {
			value = f(name)
		}
		return value
	})
	replaceRegex := regexp.MustCompile(`\{\{\{\s*([-_\/\.\w]+)\s*\}\}\}`)
	template = replaceRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := replaceRegex.FindStringSubmatch(match)[1]
		value := match
		if o, ok := context[name]; ok {
			if f, ok := o.(func() string); ok {
				value = f()
			}
			if v, ok := o.(string); ok {
				value = v
			}
		}
		return value
	})
	escapeRegex := regexp.MustCompile(`\{\{\s*([-_\/\.\w]+)\s*\}\}`)
	template = escapeRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := escapeRegex.FindStringSubmatch(match)[1]
		value := match
		if o, ok := context[name]; ok {
			if f, ok := o.(func() string); ok {
				value = f()
			}
			if v, ok := o.(string); ok {
				value = v
			}
		}
		return html.EscapeString(value)
	})
	return template
}

func mustReadFile(path string) []byte {
	file, e := ioutil.ReadFile(path)
	if e != nil {
		panic(e)
	}
	return file
}

func localhost(request *http.Request) bool {
	domain := strings.Split(request.Host, ":")[0]
	return domain == "localhost" || domain == "127.0.0.1"
}

func truncate(text string, length int) string {
	closeTags := make(map[int]string)
	position := 0
	index := 0
	for position < length && index < len(text) {
		if text[index] == '<' {
			if closeTag, ok := closeTags[index]; ok {
				delete(closeTags, index)
				index += len(closeTag)
			} else {
				index++
				match := regexp.MustCompile(`(\w+)[^>]*>`).FindStringSubmatch(text[index:])
				if len(match) > 0 {
					index--
					tag := match[1]
					if tag == "pre" || tag == "code" || tag == "img" {
						break
					}
					index += len(match[0])
					closeTag := "</" + tag + ">"
					closeIndex := strings.Index(text[index:], closeTag)
					if closeIndex >= 0 {
						closeTags[index+closeIndex] = closeTag
					}
				} else {
					position++
				}
			}
		} else if text[index] == '&' {
			index++
			entityRegex := regexp.MustCompile(`(#?[A-Za-z0-9]+;)`)
			if entity := entityRegex.FindString(text[index:]); len(entity) > 0 {
				index += len(entity)
			}
			position++
		} else {
			next := text[index:]
			skip := strings.Index(next, "<")
			if skip == -1 {
				skip = strings.Index(next, "&")
			}
			if skip == -1 {
				skip = index + length
			}
			if skip > length-position {
				skip = length - position
			}
			if skip > len(text)-index {
				skip = len(text) - index
			}
			index += skip
			position += skip
		}
	}
	var output []string
	output = append(output, text[0:index])
	if position == length {
		output = append(output, "&hellip;")
	}
	var keys []int
	for key := range closeTags {
		keys = append(keys, key)
	}
	sort.Sort(sort.IntSlice(keys))
	for key := range keys {
		if closeTag, ok := closeTags[key]; ok {
			output = append(output, closeTag)
		}
	}
	return strings.Join(output, "")
}

func posts() []string {
	fileInfos, _ := ioutil.ReadDir("blog/")
	var files []string
	for i := len(fileInfos) - 1; i >= 0; i-- {
		file := fileInfos[i].Name()
		if path.Ext(file) == ".html" {
			files = append(files, file)
		}
	}
	return files
}

func loadPost(path string) map[string]string {
	if stat, e := os.Stat(path); !os.IsNotExist(e) && !stat.IsDir() {
		file, e := os.Open(path)
		if e != nil {
			panic(e)
		}
		entry := make(map[string]string)
		var content []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "---") {
				for scanner.Scan() {
					line := scanner.Text()
					if strings.HasPrefix(line, "---") {
						break
					}
					index := strings.Index(line, ":")
					if index >= 0 {
						name := strings.Trim(strings.Trim(line[0:index], " "), "\"")
						value := strings.Trim(line[index+1:], " ")
						entry[name] = value
					}
				}
			} else {
				content = append(content, line)
			}
		}
		for scanner.Scan() {
			content = append(content, scanner.Text())
		}
		entry["content"] = strings.Join(content, "\n")
		if e := scanner.Err(); e != nil {
			panic(e)
		}
		file.Close()
		return entry
	}
	return nil
}

func renderBlog(draft bool, start int) string {
	var output []string
	length := 10
	index := 0
	files := posts()
	for len(files) > 0 && index < start+length {
		file := files[0]
		files = files[1:]
		entry := loadPost("blog/" + file)
		if entry != nil && draft || entry["state"] == "post" {
			if index >= start {
				location := "/blog/" + strings.TrimSuffix(path.Base(file), ".html")
				date, _ := time.Parse("2006-01-02 15:04:05 MST", entry["date"])
				entry["date"] = date.Format("Jan 2, 2006")
				var post []string
				post = append(post, "<div class='item'>")
				post = append(post, "<div class='date'>"+entry["date"]+"</div>\n")
				post = append(post, "<h1><a href='"+location+"'>"+entry["title"]+"</a></h1>\n")
				content := entry["content"]
				content = regexp.MustCompile(`\s\s`).ReplaceAllString(content, " ")
				truncated := truncate(content, 320)
				post = append(post, "<p>"+truncated+"</p>\n")
				if truncated != content {
					post = append(post, "<div class='more'><a href='"+location+"'>"+"Read more&hellip;"+"</a></div>\n")
				}
				post = append(post, "</div>")
				output = append(output, strings.Join(post, ""))
				output = append(output, "\n")
			}
			index++
		}
	}
	if len(files) > 0 {
		template := string(mustReadFile("./stream.html"))
		context := make(map[string]interface{})
		context["url"] = "/blog?id=" + strconv.Itoa(index)
		data := mustache(template, context, nil)
		output = append(output, data)
	}
	return strings.Join(output, "\n")
}

func rootHandler(response http.ResponseWriter, request *http.Request) {
	http.Redirect(response, request, "/", http.StatusFound)
}

func atomHandler(response http.ResponseWriter, request *http.Request) {
	host := "http" + "://" + request.Host // TODO http vs. https
	var output []string
	output = append(output, "<?xml version='1.0' encoding='UTF-8'?>")
	output = append(output, "<feed xmlns='http://www.w3.org/2005/Atom'>")
	output = append(output, "<title>"+configuration["name"].(string)+"</title>")
	output = append(output, "<id>"+host+"/</id>")
	output = append(output, "<icon>"+host+"/favicon.ico</icon>")
	output = append(output, "<updated>"+time.Now().UTC().Format("2006-01-02T15:04:05.999Z07:00")+"</updated>")
	output = append(output, "<author><name>"+configuration["name"].(string)+"</name></author>")
	output = append(output, "<link rel='alternate' type='text/html' href='"+host+"/' />")
	output = append(output, "<link rel='self' type='application/atom+xml' href='"+host+"/blog/atom.xml' />")
	files := posts()
	for _, file := range files {
		draft := localhost(request)
		entry := loadPost("blog/" + file)
		if entry != nil && (draft || entry["state"] == "post") {
			url := host + "/blog/" + strings.TrimSuffix(path.Base(file), ".html")
			output = append(output, "<entry>")
			output = append(output, "<id>"+url+"</id>")
			if author, ok := entry["author"]; ok && author != configuration["name"].(string) {
				output = append(output, "<author><name>"+author+"</name></author>")
			}
			date, _ := time.Parse("2006-01-02 15:04:05 MST", entry["date"])
			output = append(output, "<published>"+date.Format("2006-01-02T15:04:05.999Z07:00")+"</published>")
			updated := date
			if u, ok := entry["updated"]; ok {
				updated, _ = time.Parse("2006-01-02 15:04:05 MST", u)
			}
			output = append(output, "<updated>"+updated.Format("2006-01-02T15:04:05.999Z07:00")+"</updated>")
			output = append(output, "<title type='text'>"+entry["title"]+"</title>")
			output = append(output, "<content type='html'>"+html.EscapeString(entry["content"])+"</content>")
			output = append(output, "<link rel='alternate' type='text/html' href='"+url+"' title='"+entry["title"]+"' />")
			output = append(output, "</entry>")
		}
	}
	output = append(output, "</feed>")
	var data = strings.Join(output, "\n")
	response.Header().Set("Content-Type", "application/atom+xml")
	if request.Method != "HEAD" {
		length, _ := io.WriteString(response, data)
		response.Header().Set("Content-Length", strconv.Itoa(length))
	}
}

func postHandler(response http.ResponseWriter, request *http.Request) {
	pathname := strings.ToLower(request.URL.Path) // TODO normalize path
	localPath := strings.TrimPrefix(pathname, "/")
	entry := loadPost(localPath + ".html")
	if entry != nil {
		date, _ := time.Parse("2006-01-02 15:04:05 MST", entry["date"])
		entry["date"] = date.Format("Jan 2, 2006")
		if _, ok := entry["author"]; !ok {
			entry["author"] = configuration["name"].(string)
		}
		context := make(map[string]interface{})
		for key, value := range configuration {
			context[key] = value
		}
		for key, value := range entry {
			context[key] = value
		}
		template := string(mustReadFile("./post.html"))
		data := mustache(template, context, func(name string) string {
			return string(mustReadFile(path.Join("./", name)))
		})
		response.Header().Set("Content-Type", "text/html")
		if request.Method != "HEAD" {
			length, _ := io.WriteString(response, data)
			response.Header().Set("Content-Length", strconv.Itoa(length))
		}
	} else {
		extension := path.Ext(localPath)
		contentType := mime.TypeByExtension(extension)
		if len(contentType) > 0 {
			defaultHandler(response, request)
		} else {
			rootHandler(response, request)
		}
	}
}

func blogHandler(response http.ResponseWriter, request *http.Request) {
	id := request.URL.Query().Get("id")
	if start, e := strconv.Atoi(id); e == nil {
		data := renderBlog(localhost(request), start)
		response.Header().Set("Content-Type", "text/html")
		length, _ := io.WriteString(response, data)
		response.Header().Set("Content-Length", strconv.Itoa(length))
	} else {
		rootHandler(response, request)
	}
}

func defaultHandler(response http.ResponseWriter, request *http.Request) {
	pathname := strings.ToLower(request.URL.Path) // TODO normalize path
	if strings.HasSuffix(pathname, "/index.html") {
		http.Redirect(response, request, "/"+strings.TrimLeft(pathname[0:len(pathname)-11], "/"), http.StatusMovedPermanently)
	} else {
		localPath := pathname
		if strings.HasSuffix(pathname, "/") {
			localPath = path.Join(pathname, "index.html")
		}
		localPath = strings.TrimLeft(localPath, "/")
		extension := filepath.Ext(localPath)
		contentType := mime.TypeByExtension(extension)
		if len(contentType) > 0 && extension != ".html" {
			if stat, e := os.Stat(localPath); os.IsNotExist(e) {
				response.WriteHeader(http.StatusNotFound)
			} else if stat.IsDir() {
				http.Redirect(response, request, "/", http.StatusFound)
			} else {
				data := mustReadFile("./" + localPath)
				if request.Method != "HEAD" {
					response.Write(data)
				}
				response.Header().Set("Content-Type", contentType)
				response.Header().Set("Content-Length", strconv.Itoa(len(data)))
				response.Header().Set("Cache-Control", "private, max-age=0")
				response.Header().Set("Expires", "-1")
			}
		} else {
			if stat, e := os.Stat(localPath); os.IsNotExist(e) {
				if localPath != "index.html" {
					http.Redirect(response, request, path.Dir(pathname), http.StatusFound)
				} else {
					rootHandler(response, request)
				}
			} else if stat.IsDir() || extension != ".html" {
				http.Redirect(response, request, pathname+"/", http.StatusFound)
			} else {
				template := mustReadFile(path.Join("./", localPath))
				context := make(map[string]interface{})
				for key, value := range configuration {
					context[key] = value
				}
				if feed, ok := context["feed"]; !ok || len(feed.(string)) == 0 {
					context["feed"] = func() string {
						return "http" + "://" + request.Host + "/blog/atom.xml" // TODO http vs. https
					}
				}
				context["links"] = func() string {
					var list []string
					for _, link := range configuration["links"].([]interface{}) {
						name := link.(map[string]interface{})["name"].(string)
						symbol := link.(map[string]interface{})["symbol"].(string)
						url := link.(map[string]interface{})["url"].(string)
						list = append(list, "<a class='icon' target='_blank' href='"+url+"' title='"+name+"'><span class='symbol'>"+symbol+"</span></a>")
					}
					return strings.Join(list, "\n")
				}
				context["tabs"] = func() string {
					var list []string
					for _, link := range configuration["pages"].([]interface{}) {
						name := link.(map[string]interface{})["name"].(string)
						url := link.(map[string]interface{})["url"].(string)
						list = append(list, "<li class='tab'><a href='"+url+"'>"+name+"</a></li>")
					}
					return strings.Join(list, "\n")
				}
				context["blog"] = func() string {
					return renderBlog(localhost(request), 0)
				}
				data := mustache(string(template), context, func(name string) string {
					return string(mustReadFile(path.Join("./", name)))
				})
				response.Header().Set("Content-Type", "text/html")
				if request.Method != "HEAD" {
					length, _ := io.WriteString(response, data)
					response.Header().Set("Content-Length", strconv.Itoa(length))
				}
			}
		}
	}
}

func letsEncryptHandler(response http.ResponseWriter, request *http.Request) {
	pathname := request.URL.Path // TODO normalize path
	localPath := strings.TrimLeft(pathname, "/")
	if stat, e := os.Stat(localPath); !os.IsNotExist(e) && !stat.IsDir() {
		data := mustReadFile(localPath)
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.Header().Set("Content-Length", strconv.Itoa(len(data)))
		response.Write(data)
	} else {
		rootHandler(response, request)
	}
}

type loggerHandler struct {
	handler http.Handler
}

func (logger loggerHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	fmt.Println(request.Method + " " + request.URL.Path)
	logger.handler.ServeHTTP(response, request)
}

func main() {
	fmt.Println(runtime.Version())
	file := mustReadFile("./app.json")
	if e := json.Unmarshal(file, &configuration); e != nil {
		panic(e)
	}
	http.HandleFunc("/.git", rootHandler)
	http.HandleFunc("/admin", rootHandler)
	http.HandleFunc("/admin.cfg", rootHandler)
	http.HandleFunc("/app.go", rootHandler)
	http.HandleFunc("/app.js", rootHandler)
	http.HandleFunc("/app.json", rootHandler)
	http.HandleFunc("/header.html", rootHandler)
	http.HandleFunc("/meta.html", rootHandler)
	http.HandleFunc("/package.json", rootHandler)
	http.HandleFunc("/post.css", rootHandler)
	http.HandleFunc("/post.html", rootHandler)
	http.HandleFunc("/site.css", rootHandler)
	http.HandleFunc("/stream.html", rootHandler)
	http.HandleFunc("/web.config", rootHandler)
	http.HandleFunc("/blog/atom.xml", atomHandler)
	http.HandleFunc("/blog/", postHandler)
	http.HandleFunc("/blog", blogHandler)
	http.HandleFunc("/.well-known/acme-challenge/", letsEncryptHandler)
	http.HandleFunc("/", defaultHandler)
	port := 8080
	fmt.Println("http://localhost:" + strconv.Itoa(port))
	http.ListenAndServe(":8080", loggerHandler{http.DefaultServeMux})
}